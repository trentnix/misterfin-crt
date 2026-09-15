package playback

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
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
		s.control(p, Callbacks{}, Control{Kind: SeekAudioStep, Seconds: 10}, timer)
		timer.Stop()
		if strings.Contains(commands.String(), "seek") != (kind == "Audio") || !s.state.IsPaused {
			t.Fatal("direct seek changed unsupported media or pause state")
		}
	}
	s := playbackSession{item: jellyfin.Item{Type: "Audio"}, started: true}
	var notice error
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	s.control(&playerProcess{decoder: ffplay.Decoder{}}, Callbacks{ControlError: func(err error) { notice = err }}, Control{Kind: SeekAudioStep, Seconds: 10}, timer)
	if notice == nil {
		t.Fatal("unsupported seek failed silently")
	}
}

func TestRelativeAudioSeekPreservesOffsetAndPause(t *testing.T) {
	for _, seconds := range []int{-37, 83} {
		var commands bytes.Buffer
		p := &playerProcess{decoder: mplayer.Decoder{}, control: playerapi.Control{Stdin: &commands}}
		s := playbackSession{item: jellyfin.Item{Type: "Audio"}, started: true, state: jellyfin.PlayState{IsPaused: true}}
		timer := time.NewTimer(time.Hour)
		s.control(p, Callbacks{}, Control{Kind: SeekAudioRelative, Seconds: seconds}, timer)
		timer.Stop()
		if !strings.HasPrefix(commands.String(), fmt.Sprintf("pausing_keep seek %d 0\n", seconds)) || !s.state.IsPaused {
			t.Fatal("relative audio seek changed", commands.String())
		}
		commands.Reset()
		s.control(p, Callbacks{}, Control{Kind: SeekAudioStep, Seconds: seconds}, nil)
		if commands.Len() != 0 {
			t.Fatal("local audio step accepted arbitrary offset")
		}
	}
}

func TestUnknownControlReportsFailureWithoutChangingState(t *testing.T) {
	s := playbackSession{state: jellyfin.PlayState{IsPaused: true, PositionTicks: 123}}
	before := s.state
	var failure error
	s.control(nil, Callbacks{ControlError: func(err error) { failure = err }}, Control{Kind: ControlKind("typo")}, nil)
	if failure == nil || !reflect.DeepEqual(s.state, before) {
		t.Fatal("unsupported command was ignored or changed playback")
	}
}
