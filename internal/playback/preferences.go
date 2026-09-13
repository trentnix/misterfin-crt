package playback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"misterfin-go/internal/jellyfin"
)

// Preferences stores per-video choices under the application's state directory.
// One writer coalesces changes without doing disk I/O on the playback loop.
// Call Close after all playback sessions finish to flush pending writes.
type Preferences struct {
	dir     string
	mu      sync.Mutex
	values  map[string]videoPreference
	pending map[string]videoPreference
	wake    chan struct{}
	stop    chan struct{}
	done    chan struct{}
	err     error // Read only after done closes.
}

// NewPreferences starts the writer. Files are read lazily during playback
// preparation. Missing or damaged records use the normal playback defaults.
func NewPreferences(stateDir string) *Preferences {
	p := &Preferences{
		dir:    filepath.Join(stateDir, "playback"),
		values: make(map[string]videoPreference), pending: make(map[string]videoPreference),
		wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}),
	}
	go p.writeLoop()
	return p
}

// Close flushes queued choices and reports any records that could not be saved.
// The caller must stop playback before calling Close, and call it only once.
func (p *Preferences) Close() error {
	close(p.stop)
	<-p.done
	return p.err
}

// videoPreference contains stable choices, never tokens or downloaded subtitles.
// Stream metadata prevents an old index from selecting a different track after
// a file is replaced. Picture mode remains usable when the media source changes.
type videoPreference struct {
	Version  int                  `json:"version"`
	SourceID string               `json:"source_id"`
	Picture  PictureMode          `json:"picture"`
	Audio    jellyfin.MediaStream `json:"audio"`
	Subtitle jellyfin.MediaStream `json:"subtitle"`
}

func preferenceKey(c *jellyfin.Client, item string) string {
	key, _ := json.Marshal([3]string{c.Config.Server, c.Session.UserID, item})
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])
}

// load runs on the preparation worker. Recheck memory after disk I/O so an
// immediate reopen sees a choice even if the writer has not saved it yet.
func (p *Preferences) load(key string) *videoPreference {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	value, ok := p.values[key]
	p.mu.Unlock()
	if ok {
		return &value
	}
	f, err := os.Open(filepath.Join(p.dir, key+".json"))
	if err == nil {
		defer f.Close()
		err = json.NewDecoder(io.LimitReader(f, 16*1024)).Decode(&value)
	}
	valid := err == nil && value.Version == 1 && value.Picture <= PictureZoom43
	p.mu.Lock()
	defer p.mu.Unlock()
	if latest, ok := p.values[key]; ok {
		return &latest
	}
	if !valid {
		return nil
	}
	p.values[key] = value
	return &value
}

func (v videoPreference) restore(t VideoTracks) TrackOptions {
	o := TrackOptions{Picture: v.Picture, Selection: jellyfin.TrackSelection{AudioIndex: -1, SubtitleIndex: -1}}
	if v.SourceID != t.SourceID {
		return o
	}
	if stream, ok := t.Stream("Audio", v.Audio.Index); ok && stream == v.Audio {
		o.Selection.AudioIndex = stream.Index
	}
	if stream, ok := t.Stream("Subtitle", v.Subtitle.Index); ok && stream == v.Subtitle {
		o.Selection.SubtitleIndex = stream.Index
	}
	return o
}

func (p *Preferences) save(key string, t VideoTracks) {
	if p == nil {
		return
	}
	v := videoPreference{Version: 1, SourceID: t.SourceID, Picture: t.Picture,
		Audio: jellyfin.MediaStream{Index: -1}, Subtitle: jellyfin.MediaStream{Index: -1}}
	if stream, ok := t.Stream("Audio", t.Selection.AudioIndex); ok {
		v.Audio = stream
	}
	if stream, ok := t.Stream("Subtitle", t.Selection.SubtitleIndex); ok {
		v.Subtitle = stream
	}
	p.mu.Lock()
	if old, ok := p.values[key]; ok && old == v {
		p.mu.Unlock()
		return
	}
	p.values[key] = v
	p.pending[key] = v
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Preferences) writeLoop() {
	defer close(p.done)
	failed := make(map[string]bool)
	for {
		stopping := false
		select {
		case <-p.wake:
		case <-p.stop:
			stopping = true
		}
		p.mu.Lock()
		pending := p.pending
		p.pending = make(map[string]videoPreference)
		p.mu.Unlock()
		for key, value := range pending {
			if p.write(key, value) != nil {
				failed[key] = true
			} else {
				delete(failed, key)
			}
		}
		if stopping {
			if len(failed) != 0 {
				p.err = errors.New("could not save playback choices")
			}
			return
		}
	}
}

func (p *Preferences) write(key string, value videoPreference) error {
	if err := os.MkdirAll(p.dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(p.dir, ".choices-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(value)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(p.dir, key+".json"))
}
