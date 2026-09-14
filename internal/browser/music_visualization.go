package browser

import (
	"os"
	"path/filepath"
	"time"

	"misterfin-crt/internal/musicviz"
	"misterfin-crt/internal/playback"
)

// musicPresentation owns effect selection and the latest disposable audio
// measurement. Effect animation itself belongs to RasterRenderer.
type musicPresentation struct {
	library    *musicviz.Library
	index      int
	labelUntil time.Time
	levels     playback.AudioLevels
	levelTime  time.Time
	loading    bool
	error      string
}

func (s *browserSession) loadMusicConfig() {
	path := os.Getenv("MISTERFIN_MUSIC_CONFIG")
	if path == "" {
		path = filepath.Join(filepath.Dir(s.config.ConfigPath), "music.json")
	}
	go func() {
		library, err := musicviz.LoadPresets(path)
		s.send(s.ctx, musicConfigResult{music: library, err: err})
	}()
}

func (s *browserSession) handleMusicConfig(r musicConfigResult) bool {
	if r.err != nil {
		s.model.Notice = "Could not load music.json. Check music configuration and assets."
		return true
	}
	s.music.library = r.music
	s.music.index = r.music.Index(r.music.Config.Default)
	s.loadMusicAssets()
	return true
}

func (s *browserSession) cycleMusicBackground() {
	if s.music.library == nil {
		return
	}
	s.music.index = (s.music.index + 1) % len(s.music.library.Config.Backgrounds)
	s.music.labelUntil = time.Now().Add(1500 * time.Millisecond)
	s.music.error = ""
	s.loadMusicAssets()
}

func (s *browserSession) loadMusicAssets() {
	if s.music.library == nil || s.music.loading || s.music.library.Ready(s.music.index) {
		return
	}
	library, index := s.music.library, s.music.index
	s.music.loading = true
	go func() {
		loaded, err := library.LoadAssets(index)
		s.send(s.ctx, musicAssetsResult{music: loaded, index: index, err: err})
	}()
}

func (s *browserSession) handleMusicAssets(r musicAssetsResult) bool {
	s.music.loading = false
	if r.err == nil {
		s.music.library = r.music
	} else if r.index == s.music.index {
		s.music.error = "Background unavailable. Check music assets."
	}
	if r.index != s.music.index {
		s.loadMusicAssets()
	}
	return true
}
