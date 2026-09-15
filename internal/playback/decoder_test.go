package playback

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"syscall"
	"testing"

	"misterfin-crt/internal/jellyfin"
	playerapi "misterfin-crt/internal/player"
	"misterfin-crt/internal/player/ffplay"
	"misterfin-crt/internal/player/mplayer"
	"misterfin-crt/internal/player/pythonhelper"
)

func TestDecoderSelectionAndInput(t *testing.T) {
	for _, tc := range []struct {
		name, kind         string
		options            Config
		executable, script string
		input              playerapi.Input
	}{
		{name: "native video", kind: "Movie", options: Config{Height: 240, VideoDecoder: mplayer.Decoder{Width: 640, Height: 240}, AudioDecoder: mplayer.Decoder{Width: 640, Height: 240}}, executable: "/media/fat/misterfin-crt/mplayer-arm", input: playerapi.Pipe},
		{name: "native audio", kind: "Audio", options: Config{Height: 288, VideoDecoder: mplayer.Decoder{Width: 640, Height: 288}, AudioDecoder: mplayer.Decoder{Width: 640, Height: 288}}, executable: "/media/fat/misterfin-crt/mplayer-arm", input: playerapi.URL},
		{name: "native override", kind: "Episode", options: Config{Height: 480, VideoDecoder: mplayer.Decoder{Player: "custom-mplayer", Width: 640, Height: 480}, AudioDecoder: mplayer.Decoder{Player: "custom-mplayer", Width: 640, Height: 480}}, executable: "custom-mplayer", input: playerapi.Pipe},
		{name: "desktop video", kind: "Movie", options: Config{VideoDecoder: ffplay.Decoder{}, AudioDecoder: ffplay.Decoder{}}, executable: "ffplay", input: playerapi.Pipe},
		{name: "desktop audio", kind: "Audio", options: Config{VideoDecoder: ffplay.Decoder{}, AudioDecoder: ffplay.Decoder{}}, executable: "ffplay", input: playerapi.Pipe},
		{name: "desktop executable override", kind: "Audio", options: Config{VideoDecoder: ffplay.Decoder{Player: "custom-ffplay"}, AudioDecoder: ffplay.Decoder{Player: "custom-ffplay"}}, executable: "custom-ffplay", input: playerapi.Pipe},
		{name: "inline video", kind: "Movie", options: Config{VideoDecoder: pythonhelper.Decoder{Script: "video.py", Output: "frame", Width: 640, Height: 240}, AudioDecoder: pythonhelper.Decoder{Script: "audio.py", Output: "frame", Width: 640, Height: 240}, Height: 240}, executable: "python3", script: "video.py", input: playerapi.Pipe},
		{name: "inline audio", kind: "Audio", options: Config{VideoDecoder: pythonhelper.Decoder{Script: "video.py", Output: "frame", Width: 640, Height: 288}, AudioDecoder: pythonhelper.Decoder{Script: "video.py", Output: "frame", Width: 640, Height: 288}, Height: 288}, executable: "python3", script: "video.py", input: playerapi.URL},
		{name: "independent audio helper", kind: "Audio", options: Config{VideoDecoder: pythonhelper.Decoder{Script: "video.py", Output: "frame", Width: 640, Height: 240}, AudioDecoder: pythonhelper.Decoder{Script: "audio.py", Output: "frame", Width: 640, Height: 240}, Height: 240}, executable: "python3", script: "audio.py", input: playerapi.URL},
		{name: "audio helper without video geometry", kind: "Audio", options: Config{VideoDecoder: mplayer.Decoder{}, AudioDecoder: pythonhelper.Decoder{Script: "audio.py"}}, executable: "python3", script: "audio.py", input: playerapi.URL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := jellyfin.Item{Type: tc.kind}
			d, err := selectDecoder(tc.options, item, PictureOriginal)
			if err != nil {
				t.Fatal(err)
			}
			if d.Executable() != tc.executable || d.Input(item) != tc.input {
				t.Fatalf("incorrect decoder: %T, executable=%q, input=%v", d, d.Executable(), d.Input(item))
			}
			source := ""
			if tc.input == playerapi.URL {
				source = "http://127.0.0.1:1234/audio"
			}
			args := d.Args(item, source)
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
		options    Config
	}{
		{name: "missing decoder", kind: "Movie", options: Config{VideoDecoder: nil, AudioDecoder: mplayer.Decoder{}}},
		{name: "missing Python helper", kind: "Movie", options: Config{VideoDecoder: pythonhelper.Decoder{}, AudioDecoder: mplayer.Decoder{}}},
		{name: "unsupported item", kind: "Photo", options: Config{VideoDecoder: ffplay.Decoder{}, AudioDecoder: ffplay.Decoder{}}},
		{name: "native width", kind: "Movie", options: Config{Height: 240, VideoDecoder: mplayer.Decoder{Width: 320, Height: 240}, AudioDecoder: mplayer.Decoder{Width: 320, Height: 240}}},
		{name: "native height", kind: "Movie", options: Config{Height: 360, VideoDecoder: mplayer.Decoder{Width: 640, Height: 360}, AudioDecoder: mplayer.Decoder{Width: 640, Height: 360}}},
		{name: "inline missing output", kind: "Movie", options: Config{VideoDecoder: pythonhelper.Decoder{Script: "video.py", Width: 640, Height: 240}, AudioDecoder: pythonhelper.Decoder{Script: "video.py", Width: 640, Height: 240}, Height: 240}},
		{name: "inline interlaced", kind: "Movie", options: Config{VideoDecoder: pythonhelper.Decoder{Script: "video.py", Output: "frame", Width: 640, Height: 480}, AudioDecoder: pythonhelper.Decoder{Script: "video.py", Output: "frame", Width: 640, Height: 480}, Height: 480}},
		{name: "missing audio helper", kind: "Audio", options: Config{VideoDecoder: pythonhelper.Decoder{}, AudioDecoder: pythonhelper.Decoder{}, Height: 240}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := selectDecoder(tc.options, jellyfin.Item{Type: tc.kind}, PictureOriginal); err == nil {
				t.Fatal("invalid mode accepted")
			}
		})
	}
}

// Only the decoder for the requested media type needs to be usable.
func TestDecoderChoicesAreIndependent(t *testing.T) {
	for _, kind := range []string{"Movie", "Audio"} {
		o := Config{VideoDecoder: nil, AudioDecoder: ffplay.Decoder{}}
		if kind == "Movie" {
			o.VideoDecoder, o.AudioDecoder = o.AudioDecoder, o.VideoDecoder
		}
		d, err := selectDecoder(o, jellyfin.Item{Type: kind}, PictureOriginal)
		if err != nil || d.Executable() != "ffplay" {
			t.Fatalf("%s depended on the other decoder: %v", kind, err)
		}
	}
}

func TestDecoderControlProtocols(t *testing.T) {
	for _, tc := range []struct {
		name     string
		decoder  playerapi.Decoder
		commands string
		signals  []syscall.Signal
	}{
		{name: "mplayer", decoder: mplayer.Decoder{}, commands: "pause\npause\npausing_keep_force get_time_pos\npausing_keep_force osd_show_text \" \" 1\n"},
		{name: "python", decoder: pythonhelper.Decoder{}, commands: "pause true\npause false\n"},
		{name: "ffplay", decoder: ffplay.Decoder{}, signals: []syscall.Signal{syscall.SIGSTOP, syscall.SIGCONT}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var commands bytes.Buffer
			var signals []syscall.Signal
			p := playerProcess{decoder: tc.decoder, control: playerapi.Control{
				Stdin:  &commands,
				Signal: func(signal syscall.Signal) error { signals = append(signals, signal); return nil },
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
			p.control.Stdin = failedDecoderWriter{}
			p.control.Signal = func(syscall.Signal) error { return io.ErrClosedPipe }
			if !errors.Is(p.pause(true), io.ErrClosedPipe) {
				t.Fatal("pause transport error was lost")
			}
		})
	}
}

type failedDecoderWriter struct{}

func (failedDecoderWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
