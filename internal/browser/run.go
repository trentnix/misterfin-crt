package browser

import (
	"bytes"
	"context"
	"errors"
	"misterfin-go/internal/input"
	"misterfin-go/internal/jellyfin"
	"misterfin-go/internal/playback"
	"misterfin-go/internal/videoout"
	"strings"
	"time"
)

type result struct {
	request         Request
	page            jellyfin.Page
	err             error
	client          *jellyfin.Client
	code            string
	auth            bool
	update          artUpdate
	imageID         int
	art             bool
	neighbor        bool
	mediaGeneration int
	parent          View
	item            *jellyfin.Item
}

func Run(ctx context.Context, configPath, stateDir string, player playback.Options, output videoout.Output, renderer Renderer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer output.Clear()
	keys, done, err := input.Read(ctx, player.Headless)
	if err != nil {
		return err
	}
	defer func() { cancel(); <-done }()
	events := make(chan result, 16)
	send := func(work context.Context, r result) {
		select {
		case events <- r:
		case <-work.Done():
		}
	}
	mediaGeneration := 0
	var mediaCancel context.CancelFunc = func() {}
	defer func() { mediaCancel() }()
	mediaPending := false
	nextTrack := 0
	var queuedNeighbor *result
	m := New()
	var client *jellyfin.Client
	status := "Connecting to Jellyfin..."
	authGeneration := 0
	var workCancel context.CancelFunc = func() {}
	defer func() { workCancel() }()
	var artCancel context.CancelFunc = func() {}
	defer func() { artCancel() }()
	var art Artwork
	var artwork *artworkLoader
	selectedKey := ""
	artError := ""
	imageID := 0
	authenticate := func() {
		workCancel()
		artCancel()
		imageID++
		art = Artwork{}
		artError = ""
		selectedKey = ""
		authGeneration++
		generation := authGeneration
		work, stop := context.WithCancel(ctx)
		workCancel = stop
		status = "Connecting to Jellyfin..."
		go func() {
			c, err := jellyfin.LoadConfig(configPath)
			var jf *jellyfin.Client
			if err == nil {
				var s jellyfin.Session
				s, err = jellyfin.LoadSession(stateDir, c.Server)
				if err == nil {
					jf = jellyfin.NewClient(c, s)
					err = jf.Authenticate(work, stateDir, func(code string) {
						send(work, result{auth: true, request: Request{Generation: generation}, code: code})
					})
				}
			}
			send(work, result{auth: true, request: Request{Generation: generation}, client: jf, err: err})
		}()
	}
	load := func(req *Request) {
		if req == nil {
			return
		}
		workCancel()
		work, stop := context.WithCancel(ctx)
		workCancel = stop
		jf := client
		go func() {
			var p jellyfin.Page
			var err error
			if req.Location.Kind == "views" {
				p, err = jf.Libraries(work)
			} else {
				p, err = jf.List(work, req.Location, req.Start, PageSize)
			}
			send(work, result{request: *req, page: p, err: err})
		}()
	}
	loadArt := func() {
		item := m.Current().Item()
		key := ""
		root := len(m.Stack) == 1
		detail := m.Current().Detail != nil
		if item != nil {
			key = item.ID
			if root {
				key = "root:" + key
			}
			if detail {
				key = "detail:" + key
			}
		}
		if key == selectedKey {
			return
		}
		selectedKey = key
		artCancel()
		imageID++
		art = Artwork{}
		artError = ""
		if item == nil || client == nil {
			return
		}
		art = artwork.snapshot(*item, root)
		selected := *item
		generation := imageID
		work, stop := context.WithCancel(ctx)
		artCancel = stop
		loader := artwork
		go loader.load(work, selected, root, detail, func(update artUpdate) {
			send(work, result{art: true, imageID: generation, update: update})
		})
	}

	geometry := output.Geometry()
	m.Rows = visibleRows(geometry.Width, geometry.Height)
	navigateMedia := func(direction int) {
		if len(m.Stack) < 2 || m.Current().Detail == nil || mediaPending {
			return
		}
		mediaCancel()
		mediaGeneration++
		generation := mediaGeneration
		parent := m.Stack[len(m.Stack)-2]
		kind := m.Current().Detail.Type
		rows := m.Rows
		work, stop := context.WithCancel(ctx)
		mediaCancel = stop
		mediaPending = true
		go func() {
			parent, item, err := adjacentMedia(work, client, parent, kind, direction, rows)
			send(work, result{neighbor: true, mediaGeneration: generation, parent: parent, item: item, err: err})
		}()
	}
	driver := playbackDriver{ctx: ctx, options: player, output: output, events: make(chan PlaybackEvent, 16)}
	controller := newPlaybackController(func(item jellyfin.Item, offset *int64, gate <-chan struct{}, prepared bool, controls chan playback.Control) playbackProcess {
		return driver.launch(client, item, offset, gate, prepared, controls)
	})
	m.PlaybackState = controller.state
	defer func() { cancel(); controller.Close() }()
	startPlayback := func(startTicks *int64, paused bool) {
		selected := *m.Current().Detail
		if selected.Type != "Audio" {
			output.Clear()
			artCancel()
			imageID++
		}
		controller.Start(selected, startTicks, paused, time.Now())
		nextTrack = 0
		m.Notice = ""
	}
	var lastVideoOverlay []byte
	frameInterval := time.Second / 60
	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()
	draw := func() error {
		now := time.Now()
		// Browser motion follows the C client's 60 Hz timeline. Video owns its
		// decoding cadence and only needs the existing 30 Hz overlay updates.
		interval := time.Second / 60
		if controller.running && m.PlayingVideo {
			interval = time.Second / 30
		}
		if interval != frameInterval {
			frameInterval = interval
			ticker.Reset(interval)
		}
		scene := sceneFromModel(m, status, art, artError, now)
		scene.Video = controller.running && m.PlayingVideo
		scene.Playback = controller.Snapshot(now)
		frame := renderer.Render(geometry.Width, geometry.Height, scene)
		changed := !bytes.Equal(lastVideoOverlay, frame.Overlay)
		lastVideoOverlay = append(lastVideoOverlay[:0], frame.Overlay...)
		if err := output.Present(frame); err != nil {
			return err
		}
		if frame.Video && changed && m.Paused {
			controller.Refresh()
		}
		return nil
	}
	authenticate()
	if err := draw(); err != nil {
		return err
	}
	for {
		select {
		case <-ticker.C:
			controller.Tick(time.Now())
		case <-ctx.Done():
			return nil
		case key, ok := <-keys:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("terminal input closed")
			}
			if strings.HasSuffix(key, "-repeat") {
				photo := m.Current().Detail != nil && m.Current().Detail.Type == "Photo"
				if key == "up-repeat" && (controller.running || photo) {
					continue
				}
				key = strings.TrimSuffix(key, "-repeat")
			}
			if m.PlayingAudio && controller.running && key != "quit" {
				now := time.Now()
				switch key {
				case "back":
					controller.Key("back", now)
					mediaCancel()
					mediaGeneration++
					mediaPending = false
					queuedNeighbor = nil
					nextTrack = 0
				case "open":
					controller.Key("open", now)
				case "up":
					controller.Key("up", now)
				case "previous", "next":
					nextTrack = -1
					if key == "next" {
						nextTrack = 1
					}
					navigateMedia(nextTrack)
				}
				if err := draw(); err != nil {
					return err
				}
				continue
			}
			if controller.running && key != "quit" {
				controller.Key(key, time.Now())
				if !controller.running {
					output.Clear()
				}
				if err := draw(); err != nil {
					return err
				}
				continue
			}
			if mediaPending && key != "quit" {
				if key == "back" {
					mediaCancel()
					mediaGeneration++
					mediaPending = false
					m.PlayingAudio = false
					m.Notice = ""
					load(m.Key("back"))
					loadArt()
				}
				continue
			}
			if m.Current().Detail != nil && m.Current().Detail.Type == "Photo" {
				if key == "back" {
					m.Notice = ""
					m.HideControls()
				}
				switch key {
				case "up":
					m.ToggleControls(time.Now())
				case "previous", "next", "down":
					direction := 1
					if key == "previous" {
						direction = -1
					}
					navigateMedia(direction)
				}
			}

			if key == "quit" {
				return nil
			}
			if status != "" {
				if key == "back" {
					return nil
				}
				if key == "retry" || key == "open" {
					authenticate()
				}
			} else {
				if key == "select" && m.Notice == "" && resumableVideo(m.Current().Detail) {
					start := int64(0)
					startPlayback(&start, false)
					continue
				}
				if key == "open" && m.Notice == "" && m.Current().Detail != nil && playback.Supported(*m.Current().Detail) {
					startPlayback(nil, false)
					continue
				}
				if key == "retry" {
					if item := m.Current().Item(); item != nil {
						artwork.forget(*item)
					}
					selectedKey = ""
				}
				before := m.Generation
				wasDetail := m.Current().Detail != nil
				req := m.Key(key)
				if key == "open" && m.Current().Detail != nil && m.Current().Detail.Type == "Photo" {
					m.HideControls()
				}
				if m.Quit {
					return nil
				}
				if m.Generation != before {
					workCancel()
				}
				load(req)
				if key == "open" && !wasDetail && m.Current().Detail != nil && jellyfin.IsLive(*m.Current().Detail) {
					startPlayback(nil, false)
					continue
				}
				loadArt()
				if key == "open" && !wasDetail && m.Current().Detail != nil && m.Current().Detail.Type == "Audio" {
					startPlayback(nil, false)
				}
			}
		case event := <-driver.events:
			ended := controller.Handle(event, time.Now())
			if controller.notice != "" {
				m.Notice = controller.notice
			}
			if !controller.running {
				output.Clear()
			}
			if !ended {
				continue
			}
			m.Notice = ""
			if m.PlayingAudio && !controller.stoppedByUser && event.Err == nil && queuedNeighbor != nil {
				m.Stack[len(m.Stack)-2] = queuedNeighbor.parent
				m.Current().Detail = queuedNeighbor.item
				m.Current().Title = queuedNeighbor.item.Name
				queuedNeighbor = nil
				selectedKey = ""
				loadArt()
				startPlayback(nil, false)
			} else if m.PlayingAudio && !controller.stoppedByUser && event.Err == nil {
				direction := nextTrack
				if direction == 0 {
					direction = 1
				}
				navigateMedia(direction)
			} else {
				wasAudio := m.PlayingAudio
				m.PlayingAudio = false
				m.HideControls()
				if (wasAudio && controller.stoppedByUser) || (m.Current().Detail != nil && jellyfin.IsLive(*m.Current().Detail)) {
					load(m.Key("back"))
				}
				selectedKey = ""
				loadArt()
				if event.Err != nil {
					m.Notice = event.Err.Error() + "  A:back"
				}
			}
		case r := <-events:
			if r.neighbor {
				if r.mediaGeneration != mediaGeneration {
					continue
				}
				mediaPending = false
				nextTrack = 0
				if controller.running && m.PlayingAudio {
					if r.item != nil && r.err == nil {
						queuedNeighbor = &r
						controller.StopForTrackChange()
					}
					if r.err != nil {
						m.Notice = "Could not load adjacent track"
					}
					continue
				}
				if r.err != nil {
					m.Notice = "Could not load adjacent item. A:back"
					m.PlayingAudio = false
				} else if r.item != nil {
					m.Stack[len(m.Stack)-2] = r.parent
					m.Current().Detail = r.item
					m.Current().Title = r.item.Name
					m.Notice = ""
					selectedKey = ""
					loadArt()
					if r.item.Type == "Audio" {
						startPlayback(nil, false)
					}
				} else if m.PlayingAudio {
					m.PlayingAudio = false
					load(m.Key("back"))
					loadArt()
				}
			} else if r.auth {
				if r.request.Generation != authGeneration {
					continue
				}
				if r.code != "" {
					status = "Quick Connect: " + r.code + "\nApprove this code in your Jellyfin client. Waiting for sign-in..."
				} else if r.err != nil {
					status = r.err.Error()
				} else {
					client = r.client
					m = New()
					m.PlaybackState = controller.state
					m.Rows = visibleRows(geometry.Width, geometry.Height)
					selectedKey = ""
					artwork = newArtworkLoader(client)
					artwork.photoWidth, artwork.photoHeight = geometry.Width, geometry.Height
					status = ""
					load(m.Load(0))
				}
			} else if r.art {
				if r.imageID != imageID {
					continue
				}
				if jellyfin.Rejected(r.update.err) {
					status = "Session rejected. Press R to sign in again."
					continue
				}
				if r.update.err != nil {
					switch r.update.kind {
					case "detail":
						artError = "Details unavailable. R:retry"
					case "count":
						artError = "Library count unavailable. R:retry"
					default:
						artError = "Artwork unavailable. R:retry"
					}
				} else {
					applyArtwork(&art, r.update)
					if r.update.kind == "detail" && m.Current().Detail != nil {
						m.Current().Detail = r.update.detail
					}
				}

			} else if m.Apply(r.request, r.page, r.err) {
				if jellyfin.Rejected(r.err) {
					artCancel()
					imageID++
					status = "Session rejected. Press R to sign in again."
				} else {
					loadArt()
				}
			} else {
				continue
			}
		}
		if err := draw(); err != nil {
			return err
		}
	}
}
