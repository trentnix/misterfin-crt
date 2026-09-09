package browser

import (
	"context"
	"errors"
	"math"
	"misterfin-go/internal/jellyfin"
	"misterfin-go/internal/platform"
	"misterfin-go/internal/playback"
	"misterfin-go/internal/terminal"
	"time"
)

type result struct {
	request  Request
	page     jellyfin.Page
	err      error
	client   *jellyfin.Client
	code     string
	auth     bool
	update   artUpdate
	imageID  int
	art      bool
	playback bool
	position bool
	ticks    int64
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
	var playCancel context.CancelFunc = func() {}
	var playDone chan struct{}
	defer func() {
		playCancel()
		if playDone != nil {
			<-playDone
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
	start := time.Now()
	last := start
	anim := Animation{}
	ticker := time.NewTicker(time.Second / 30)
	defer ticker.Stop()
	draw := func() error {
		if playing && !m.PlayingAudio && (!player.Headless || player.TerminalPlayer != "") {
			return nil
		}
		now := time.Now()
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
		case <-ctx.Done():
			return nil
		case key, ok := <-keys:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("terminal input closed")
			}
			if playing && key != "quit" {
				if key == "back" {
					playCancel()
					m.Notice = "Stopping playback..."
				}
				continue
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
					selected := *m.Current().Detail
					m.PlayingAudio = selected.Type == "Audio"
					m.PositionTicks = 0
					if !m.PlayingAudio {
						artCancel()
						imageID++
					}
					playCtx, stop := context.WithCancel(ctx)
					playCancel = stop
					if !m.PlayingAudio && (!player.Headless || player.TerminalPlayer != "") {
						if err := d.Present(make([]byte, geometry.Width*geometry.Height*4)); err != nil {
							return err
						}
					}
					playDone = make(chan struct{})
					finished := playDone
					playing = true
					m.Notice = "Playing in video window. A:stop"
					if m.PlayingAudio {
						m.Notice = ""
					}
					go func() {
						defer close(finished)
						err := playback.Run(playCtx, client, selected, player, func(ticks int64) {
							select {
							case events <- result{position: true, ticks: ticks}:
							default:
							}
						})
						select {
						case events <- result{playback: true, err: err}:
						case <-ctx.Done():
						}
					}()
					continue
				}
				if key == "retry" {
					if item := m.Current().Item(); item != nil {
						artwork.forget(*item)
					}
					selectedKey = ""
				}
				before := m.Generation
				req := m.Key(key)
				if m.Quit {
					return nil
				}
				if m.Generation != before {
					workCancel()
				}
				load(req)
				loadArt()
			}
		case r := <-events:
			if r.position {
				if playing {
					m.PositionTicks = r.ticks
				}
			} else if r.playback {
				playing = false
				m.PlayingAudio = false
				playCancel()
				m.Notice = ""
				selectedKey = ""
				loadArt()
				if r.err != nil {
					m.Notice = r.err.Error() + "  A:back"
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
