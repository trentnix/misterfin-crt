package rendering

import (
	"time"

	"misterfin-crt/internal/ui"
)

func testScene(s Scene, p PlaybackPresentation, setup SetupPresentation, art Artwork, err string, now time.Time) Scene {
	s.Playback, s.Setup, s.Artwork, s.SelectionError, s.Now = p, setup, art, err, now
	s.Video = p.Active && !p.Audio
	return s
}

func render(w, h int, s Scene, setup SetupPresentation, art Artwork, err string, anim Animation, now time.Time) []byte {
	return renderScene(ui.New(w, h), nil, testScene(s, PlaybackPresentation{}, setup, art, err, now), anim)
}
