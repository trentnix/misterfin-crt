package browser

import (
	"context"
	"errors"
	"image"
	"misterfin-go/internal/jellyfin"
	"misterfin-go/internal/platform"
	"misterfin-go/internal/terminal"
	"time"
)

type result struct {
	request Request
	page    jellyfin.Page
	err     error
	client  *jellyfin.Client
	code    string
	auth    bool
	image   image.Image
	imageID int
	art     bool
}

func Run(ctx context.Context, d platform.Display, configPath, stateDir string) error {
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
	m := New()
	var client *jellyfin.Client
	status := "Connecting to Jellyfin..."
	authGeneration := 0
	var workCancel context.CancelFunc = func() {}
	defer func() { workCancel() }()
	var artCancel context.CancelFunc = func() {}
	defer func() { artCancel() }()
	var art image.Image
	artError := ""
	imageID := 0
	authenticate := func() {
		workCancel()
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
			p, err := jf.List(work, req.Location, req.Start, PageSize)
			send(work, result{request: *req, page: p, err: err})
		}()
	}
	loadArt := func() {
		artCancel()
		imageID++
		art = nil
		artError = ""
		item := m.Current().Item()
		if item == nil || item.ImageTags["Primary"] == "" || client == nil {
			return
		}
		selected := *item
		generation := imageID
		work, stop := context.WithCancel(ctx)
		artCancel = stop
		jf := client
		go func() {
			timer := time.NewTimer(120 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-work.Done():
				return
			case <-timer.C:
			}
			im, err := jf.Image(work, selected)
			send(work, result{art: true, imageID: generation, image: im, err: err})
		}()
	}
	geometry := d.Geometry()
	draw := func() error { return d.Present(Render(geometry.Width, geometry.Height, m, status, art, artError)) }
	authenticate()
	if err := draw(); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case key, ok := <-keys:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("terminal input closed")
			}
			if key == "quit" {
				return nil
			}
			if status != "" {
				if key == "retry" {
					authenticate()
				}
			} else {
				before := m.Generation
				req := m.Key(key)
				if m.Generation != before {
					workCancel()
				}
				load(req)
				loadArt()
			}
		case r := <-events:
			if r.auth {
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
					status = ""
					load(m.Load(0))
				}
			} else if r.art {
				if r.imageID != imageID {
					continue
				}
				art = r.image
				if r.err != nil {
					artError = "Artwork unavailable"
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
