package browser

import (
	"context"
	"errors"
	"math"
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
	artwork Artwork
	detail  *jellyfin.Item
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
	var art Artwork
	cache := make(map[string]Artwork)
	selectedKey := ""
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
		if cached, ok := cache[key]; ok && !detail {
			art = cached
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
			var bundle Artwork
			var metadata *jellyfin.Item
			var err error
			if root {
				page, e := jf.Mosaic(work, selected)
				err = e
				bundle.Count = page.TotalRecordCount
				for _, item := range page.Items {
					if work.Err() != nil {
						return
					}
					im, e := jf.Image(work, item)
					if e == nil && im != nil {
						bundle.Covers = append(bundle.Covers, im)
					}
				}
			} else {
				if detail {
					updated, e := jf.Details(work, selected.ID)
					if e == nil {
						selected = updated
						metadata = &updated
					} else {
						err = e
					}
				}
				bundle.Primary, _ = jf.Image(work, selected)
				bundle.Backdrop, _ = jf.ImageKind(work, selected, "Backdrop")
				if detail {
					bundle.Logo, _ = jf.ImageKind(work, selected, "Logo")
				}
			}
			send(work, result{art: true, imageID: generation, artwork: bundle, detail: metadata, err: err})
		}()
	}
	geometry := d.Geometry()
	m.Rows = visibleRows(geometry.Width, geometry.Height)
	start := time.Now()
	last := start
	anim := Animation{}
	ticker := time.NewTicker(time.Second / 30)
	defer ticker.Stop()
	draw := func() error {
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
				if key == "retry" {
					delete(cache, selectedKey)
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
					m.Rows = visibleRows(geometry.Width, geometry.Height)
					selectedKey = ""
					clear(cache)
					status = ""
					load(m.Load(0))
				}
			} else if r.art {
				if r.imageID != imageID {
					continue
				}
				if jellyfin.Rejected(r.err) {
					status = "Session rejected. Press R to sign in again."
					continue
				}
				art = r.artwork
				if len(cache) >= 16 {
					clear(cache)
				}
				cache[selectedKey] = art
				if r.detail != nil && m.Current().Detail != nil {
					m.Current().Detail = r.detail
				}
				if r.err != nil {
					artError = "Artwork or details unavailable. R:retry"
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
