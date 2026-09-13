package playback

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"syscall"
	"testing"

	"misterfin-crt/internal/jellyfin"
)

func TestDecoderSelectionAndInput(t *testing.T) {
	for _, tc := range []struct {
		name, kind         string
		options            Options
		executable, script string
		input              decoderInput
	}{
		{name: "native video", kind: "Movie", options: Options{Width: 640, Height: 240}, executable: "/media/fat/misterfin-crt/mplayer-arm", input: inputPipe},
		{name: "native audio", kind: "Audio", options: Options{Width: 640, Height: 288}, executable: "/media/fat/misterfin-crt/mplayer-arm", input: inputURL},
		{name: "native override", kind: "Episode", options: Options{Width: 640, Height: 480, Player: "custom-mplayer"}, executable: "custom-mplayer", input: inputPipe},
		{name: "desktop video", kind: "Movie", options: Options{Headless: true}, executable: "ffplay", input: inputPipe},
		{name: "desktop audio", kind: "Audio", options: Options{Headless: true}, executable: "ffplay", input: inputPipe},
		{name: "desktop override wins over audio helper", kind: "Audio", options: Options{Headless: true, Player: "custom-ffplay", AudioPlayer: "audio.py"}, executable: "custom-ffplay", input: inputPipe},
		{name: "inline video", kind: "Movie", options: Options{Headless: true, Width: 640, Height: 240, FrameOutput: "frame", TerminalPlayer: "video.py", AudioPlayer: "audio.py"}, executable: "python3", script: "video.py", input: inputPipe},
		{name: "inline audio", kind: "Audio", options: Options{Headless: true, Width: 640, Height: 288, FrameOutput: "frame", TerminalPlayer: "video.py"}, executable: "python3", script: "video.py", input: inputURL},
		{name: "audio helper precedence", kind: "Audio", options: Options{Headless: true, Width: 640, Height: 240, FrameOutput: "frame", TerminalPlayer: "video.py", AudioPlayer: "audio.py"}, executable: "python3", script: "audio.py", input: inputURL},
		{name: "audio helper without video geometry", kind: "Audio", options: Options{Headless: true, AudioPlayer: "audio.py"}, executable: "python3", script: "audio.py", input: inputURL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := jellyfin.Item{Type: tc.kind}
			d, err := selectDecoder(tc.options, item)
			if err != nil {
				t.Fatal(err)
			}
			if d.executable() != tc.executable || d.input(item) != tc.input {
				t.Fatalf("incorrect decoder: %T, executable=%q, input=%v", d, d.executable(), d.input(item))
			}
			source := ""
			if tc.input == inputURL {
				source = "http://127.0.0.1:1234/audio"
			}
			args := d.args(item, source)
			if tc.script != "" {
				if args[0] != tc.script {
					t.Fatalf("wrong helper: %v", args)
				}
				if source != "" && !reflect.DeepEqual(args[len(args)-2:], []string{"--source", source}) {
					t.Fatalf("helper lost its range-capable source: %v", args)
				}
			} else if source != "" && args[len(args)-1] != source {
				t.Fatalf("native audio lost its range-capable source: %v", args)
			}
		})
	}
}

func TestDecoderRejectsInvalidModes(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		options    Options
	}{
		{name: "unsupported item", kind: "Photo", options: Options{Headless: true}},
		{name: "native width", kind: "Movie", options: Options{Width: 320, Height: 240}},
		{name: "native height", kind: "Movie", options: Options{Width: 640, Height: 360}},
		{name: "inline on native", kind: "Movie", options: Options{Width: 640, Height: 240, FrameOutput: "frame", TerminalPlayer: "video.py"}},
		{name: "inline missing output", kind: "Movie", options: Options{Headless: true, Width: 640, Height: 240, TerminalPlayer: "video.py"}},
		{name: "inline interlaced", kind: "Movie", options: Options{Headless: true, Width: 640, Height: 480, FrameOutput: "frame", TerminalPlayer: "video.py"}},
		{name: "conflicting players", kind: "Audio", options: Options{Headless: true, Width: 640, Height: 240, FrameOutput: "frame", TerminalPlayer: "video.py", Player: "other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := selectDecoder(tc.options, jellyfin.Item{Type: tc.kind}); err == nil {
				t.Fatal("invalid mode accepted")
			}
		})
	}
}

func TestDecoderControlProtocols(t *testing.T) {
	for _, tc := range []struct {
		name     string
		decoder  decoder
		commands string
		signals  []syscall.Signal
	}{
		{name: "mplayer", decoder: mplayerDecoder{}, commands: "pause\npause\npausing_keep_force get_time_pos\npausing_keep_force osd_show_text \" \" 1\n"},
		{name: "python", decoder: pythonDecoder{}, commands: "pause true\npause false\n"},
		{name: "ffplay", decoder: ffplayDecoder{}, signals: []syscall.Signal{syscall.SIGSTOP, syscall.SIGCONT}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var commands bytes.Buffer
			var signals []syscall.Signal
			p := playerProcess{decoder: tc.decoder, control: decoderControl{
				stdin:  &commands,
				signal: func(signal syscall.Signal) error { signals = append(signals, signal); return nil },
			}}
			for _, paused := range []bool{true, false} {
				if err := p.pause(paused); err != nil {
					t.Fatal(err)
				}
			}
			p.poll()
			p.refresh()
			if commands.String() != tc.commands || !reflect.DeepEqual(signals, tc.signals) {
				t.Fatalf("wrong control protocol: commands=%q signals=%v", commands.String(), signals)
			}
			p.control.stdin = failedDecoderWriter{}
			p.control.signal = func(syscall.Signal) error { return io.ErrClosedPipe }
			if !errors.Is(p.pause(true), io.ErrClosedPipe) {
				t.Fatal("pause transport error was lost")
			}
		})
	}
}

type failedDecoderWriter struct{}

func (failedDecoderWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
