package browser

import (
	"context"
	"errors"
	"math"
	"misterfin-go/internal/jellyfin"
	"misterfin-go/internal/platform"
	"misterfin-go/internal/playback"
	"misterfin-go/internal/terminal"
	"os"
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
	playback        bool
	prepared        bool
	playbackID      int
	position        bool
	ticks           int64
	paused          *bool
	buffering       *bool
	neighbor        bool
	mediaGeneration int
	parent          View
	item            *jellyfin.Item
}

func Run(ctx context.Context, d platform.Display, configPath, stateDir string, player playback.Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	keys, done, err := terminal.Read(ctx)
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
	playing := false
	controls := make(chan playback.Control, 16)
	mediaGeneration := 0
	var mediaCancel context.CancelFunc = func() {}
	defer func() { mediaCancel() }()
	mediaPending := false
	nextTrack := 0
	var queuedNeighbor *result
	stoppedByUser := false
	var startPlayback func(*int64, bool)
	seekRestart := false
	restorePause := false
	playbackSequence := 0
	activePlaybackID := 0
	var activeFastCleanup chan struct{}
	pendingPlaybackID := 0
	var pendingCancel context.CancelFunc
	var pendingDone chan struct{}
	var pendingGate chan struct{}
	var pendingFastCleanup chan struct{}
	pendingTarget := int64(0)
	pendingPaused := false
	seekAutoPaused := false

	var playCancel context.CancelFunc = func() {}
	var playDone chan struct{}
	defer func() {
		cancel()
		playCancel()
		if pendingCancel != nil {
			pendingCancel()
		}
		if playDone != nil {
			<-playDone
		}
		if pendingDone != nil {
			<-pendingDone
		}
	}()
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

	geometry := d.Geometry()
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
	launchPlayback := func(startTicks *int64, gate <-chan struct{}, prepared bool) (int, context.CancelFunc, chan struct{}, chan struct{}) {
		playbackSequence++
		id := playbackSequence
		playCtx, stop := context.WithCancel(ctx)
		finished := make(chan struct{})
		fastCleanup := make(chan struct{})
		options := player
		options.StartTicks = startTicks
		options.Start = gate
		options.AsyncCleanup = fastCleanup
		options.Controls = controls
		options.Paused = func(paused bool) { send(ctx, result{paused: &paused, playbackID: id}) }
		options.Buffering = func(waiting bool) { send(ctx, result{buffering: &waiting, playbackID: id}) }
		if prepared {
			options.Ready = func() { send(ctx, result{prepared: true, playbackID: id}) }
		}
		selected := *m.Current().Detail
		go func() {
			defer close(finished)
			err := playback.Run(playCtx, client, selected, options, func(ticks int64) {
				select {
				case events <- result{position: true, ticks: ticks, playbackID: id}:
				default:
				}
			})
			send(ctx, result{playback: true, playbackID: id, err: err})
		}()
		return id, stop, finished, fastCleanup
	}
	startPlayback = func(startTicks *int64, paused bool) {
		restorePause = paused
		seekRestart = false
		m.SeekInFlight = false
		m.SeekTarget = nil
		m.SeekDeadline = time.Time{}
		selected := *m.Current().Detail
		m.PlayingAudio = selected.Type == "Audio"
		m.PlayingVideo = !m.PlayingAudio
		if m.PlayingVideo && player.TerminalPlayer != "" {
			_ = os.Remove(player.FrameOutput + ".video")
		}
		m.Paused = false
		m.PositionTicks = 0
		m.ProgressSeen = false
		m.LastAdvance = time.Now()
		m.Buffering, m.BufferingKnown = false, false
		m.HideControls()
		stoppedByUser = false
		nextTrack = 0
		// Discard controls left over from the preceding track.
		for len(controls) > 0 {
			<-controls
		}
		if !m.PlayingAudio {
			artCancel()
			imageID++
		}
		activePlaybackID, playCancel, playDone, activeFastCleanup = launchPlayback(startTicks, nil, false)
		playing = true
		m.Notice = ""
	}
	start := time.Now()
	last := start
	anim := Animation{}
	ticker := time.NewTicker(time.Second / 30)
	defer ticker.Stop()
	draw := func() error {
		now := time.Now()
		if playing && m.PlayingVideo {
			if !player.Headless {
				return nil
			}
			if player.TerminalPlayer != "" {
				frame, err := os.ReadFile(player.FrameOutput + ".video")
				if err != nil || len(frame) != geometry.Width*geometry.Height*4 {
					frame = make([]byte, geometry.Width*geometry.Height*4)
				}
				// The decoder owns the clean source. Never fold an overlay into it.
				renderVideoControls(frame, geometry.Width, geometry.Height, m, now)
				return d.Present(frame)
			}
		}
		dt := min(now.Sub(last).Seconds(), 0.05)
		last = now
		anim.Seconds = now.Sub(start).Seconds()
		target := float64(m.Current().Selected)
		row := float64(m.Current().Selected - m.Current().Scroll)
		anim.Selection += (target - anim.Selection) * (1 - math.Exp(-dt/0.035))
		if math.Abs(row-anim.Row) > float64(m.Rows) {
			anim.Row = row
		} else {
			anim.Row += (row - anim.Row) * (1 - math.Exp(-dt/0.055))
		}
		return d.Present(render(geometry.Width, geometry.Height, m, status, art, artError, anim, now))
	}
	authenticate()
	if err := draw(); err != nil {
		return err
	}
	for {
		select {
		case <-ticker.C:
			if playing && m.SeekTarget != nil && !seekRestart && !time.Now().Before(m.SeekDeadline) {
				seekRestart = true
				m.SeekInFlight = true
				pendingTarget = *m.SeekTarget
				pendingPaused = m.Paused
				seekAutoPaused = !pendingPaused
				if seekAutoPaused {
					select {
					case controls <- playback.Control{Kind: "pause"}:
					default:
					}
				}
				pendingGate = make(chan struct{})
				pendingPlaybackID, pendingCancel, pendingDone, pendingFastCleanup = launchPlayback(&pendingTarget, pendingGate, true)
			}
		case <-ctx.Done():
			return nil
		case key, ok := <-keys:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("terminal input closed")
			}
			if m.PlayingAudio && playing && key != "quit" {
				now := time.Now()
				switch key {
				case "back":
					stoppedByUser = true
					mediaCancel()
					mediaGeneration++
					mediaPending = false
					queuedNeighbor = nil
					nextTrack = 0
					playCancel()
				case "open":
					m.HideControls()
					select {
					case controls <- playback.Control{Kind: "pause"}:
					default:
					}
				case "up":
					m.RevealControls(now)
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
			if playing && key != "quit" {
				if seekRestart && key != "back" {
					continue
				}
				switch key {
				case "previous", "next":
					m.seekVideo(key, time.Now())
				case "back":
					stoppedByUser = true
					seekRestart = false
					m.SeekTarget = nil
					if pendingCancel != nil {
						pendingCancel()
						pendingCancel = nil
						pendingPlaybackID = 0
					}
					playCancel()
				case "open":
					m.HideControls()
					if restorePause {
						// Resume requested while the replacement stream is loading.
						restorePause = false
						break
					}
					select {
					case controls <- playback.Control{Kind: "pause"}:
					default:
					}
				case "up":
					m.RevealControls(time.Now())
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
					m.RevealControls(time.Now())
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
				if key == "open" && m.Notice == "" && m.Current().Detail != nil && playback.Supported(*m.Current().Detail) {
					if m.Current().Detail.Type != "Audio" && (!player.Headless || player.TerminalPlayer != "") {
						if err := d.Present(make([]byte, geometry.Width*geometry.Height*4)); err != nil {
							return err
						}
					}
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
					if err := d.Present(make([]byte, geometry.Width*geometry.Height*4)); err != nil {
						return err
					}
					startPlayback(nil, false)
					continue
				}
				loadArt()
				if key == "open" && !wasDetail && m.Current().Detail != nil && m.Current().Detail.Type == "Audio" {
					startPlayback(nil, false)
				}
			}
		case r := <-events:
			if r.prepared {
				if r.playbackID == pendingPlaybackID && activeFastCleanup != nil {
					close(activeFastCleanup)
					activeFastCleanup = nil
					playCancel()
				}
			} else if r.paused != nil {
				if playing && r.playbackID == activePlaybackID {
					m.Paused = *r.paused
					m.LastAdvance = time.Now()
				}
			} else if r.buffering != nil {
				if playing && r.playbackID == activePlaybackID {
					m.Buffering, m.BufferingKnown = *r.buffering, true
				}
			} else if r.neighbor {
				if r.mediaGeneration != mediaGeneration {
					continue
				}
				mediaPending = false
				nextTrack = 0
				if playing && m.PlayingAudio {
					if r.item != nil && r.err == nil {
						queuedNeighbor = &r
						playCancel()
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
			} else if r.position {
				if playing && r.playbackID == activePlaybackID {
					if !m.ProgressSeen || r.ticks != m.PositionTicks {
						m.LastAdvance = time.Now()
					}
					m.ProgressSeen = true
					m.PositionTicks = r.ticks
					if restorePause {
						select {
						case controls <- playback.Control{Kind: "pause"}:
							restorePause = false
						default:
						}
					}
				}
			} else if r.playback {
				if r.playbackID == pendingPlaybackID {
					if seekAutoPaused && playing {
						select {
						case controls <- playback.Control{Kind: "pause"}:
						default:
						}
					}
					seekAutoPaused = false
					pendingPlaybackID = 0
					pendingCancel, pendingDone, pendingGate, pendingFastCleanup = nil, nil, nil, nil
					m.SeekTarget = nil
					m.SeekInFlight = false
					seekRestart = false
					if r.err != nil {
						m.Notice = r.err.Error() + "  A:back"
					}
					continue
				}
				if r.playbackID != activePlaybackID {
					continue
				}
				if seekRestart && !stoppedByUser && pendingPlaybackID != 0 {
					activePlaybackID = pendingPlaybackID
					playCancel, playDone, activeFastCleanup = pendingCancel, pendingDone, pendingFastCleanup
					pendingPlaybackID = 0
					pendingCancel, pendingDone, pendingFastCleanup = nil, nil, nil
					close(pendingGate)
					pendingGate = nil
					restorePause = pendingPaused
					seekAutoPaused = false
					m.SeekTarget = nil
					m.SeekPresses = 0
					m.SeekInFlight = false
					seekRestart = false
					m.Paused = false
					m.PositionTicks = pendingTarget
					m.ProgressSeen = false
					m.LastAdvance = time.Now()
					m.Buffering, m.BufferingKnown = false, false
					continue
				}
				m.SeekTarget = nil
				m.SeekInFlight = false
				seekRestart = false
				playing = false
				m.PlayingVideo = false
				if player.TerminalPlayer != "" {
					_ = os.Remove(player.FrameOutput + ".video")
				}
				playCancel()
				m.Notice = ""
				if m.PlayingAudio && !stoppedByUser && r.err == nil && queuedNeighbor != nil {
					m.Stack[len(m.Stack)-2] = queuedNeighbor.parent
					m.Current().Detail = queuedNeighbor.item
					m.Current().Title = queuedNeighbor.item.Name
					queuedNeighbor = nil
					selectedKey = ""
					loadArt()
					startPlayback(nil, false)
				} else if m.PlayingAudio && !stoppedByUser && r.err == nil {
					direction := nextTrack
					if direction == 0 {
						direction = 1
					}
					navigateMedia(direction)
				} else {
					wasAudio := m.PlayingAudio
					m.PlayingAudio = false
					m.HideControls()
					if (wasAudio && stoppedByUser) || (m.Current().Detail != nil && jellyfin.IsLive(*m.Current().Detail)) {
						load(m.Key("back"))
					}
					selectedKey = ""
					loadArt()
					if r.err != nil {
						m.Notice = r.err.Error() + "  A:back"
					}
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
