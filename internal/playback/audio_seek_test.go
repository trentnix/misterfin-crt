package playback

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"misterfin-crt/internal/jellyfin"
	playerapi "misterfin-crt/internal/player"
	"misterfin-crt/internal/player/ffplay"
	"misterfin-crt/internal/player/mplayer"
	"misterfin-crt/internal/player/pythonhelper"
)

func TestAudioSeekCommandsAndTransportErrors(t *testing.T) {
	for _, tc := range []struct {
		decoder playerapi.AudioSeeker
		want    string
	}{
		{mplayer.Decoder{}, "pausing_keep seek 10 0\npausing_keep seek -10 0\n"},
		{pythonhelper.Decoder{}, "seek 10\nseek -10\n"},
	} {
		var commands bytes.Buffer
		for _, seconds := range []int{10, -10} {
			if err := tc.decoder.Seek(playerapi.Control{Stdin: &commands}, seconds); err != nil {
				t.Fatal(err)
			}
		}
		if commands.String() != tc.want {
			t.Fatal(commands.String())
		}
		reader, writer := io.Pipe()
		reader.Close()
		if !errors.Is(tc.decoder.Seek(playerapi.Control{Stdin: writer}, 10), io.ErrClosedPipe) {
			t.Fatal("seek error lost")
		}
		writer.Close()
	}
}

func TestOnlyStartedAudioAcceptsDirectSeek(t *testing.T) {
	for _, kind := range []string{"Movie", "TvChannel", "Audio"} {
		var commands bytes.Buffer
		p := &playerProcess{decoder: mplayer.Decoder{}, control: playerapi.Control{Stdin: &commands}}
		s := playbackSession{item: jellyfin.Item{Type: kind}, started: true, state: jellyfin.PlayState{IsPaused: true}}
		timer := time.NewTimer(time.Hour)
		s.control(p, Callbacks{}, Control{Kind: "seek", Seconds: 10}, timer)
		timer.Stop()
		if strings.Contains(commands.String(), "seek") != (kind == "Audio") || !s.state.IsPaused {
			t.Fatal("direct seek changed unsupported media or pause state")
		}
	}
	s := playbackSession{item: jellyfin.Item{Type: "Audio"}, started: true}
	var notice error
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	s.control(&playerProcess{decoder: ffplay.Decoder{}}, Callbacks{ControlError: func(err error) { notice = err }}, Control{Kind: "seek", Seconds: 10}, timer)
	if notice == nil {
		t.Fatal("unsupported seek failed silently")
	}
}
