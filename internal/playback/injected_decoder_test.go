package playback

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mistervision/internal/media"
	"mistervision/internal/player"
	"mistervision/internal/player/feedback"
	"mistervision/internal/player/ffplay"
)

// fixtureDecoder proves playback accepts a protocol it does not recognize.
// It borrows launch/control behavior but supplies an unrelated feedback format.
type fixtureDecoder struct{ ffplay.Decoder }

func (d fixtureDecoder) Name() string { return "fixture" }
func (d fixtureDecoder) WithPicture(mode player.PictureMode) player.Decoder {
	d.Picture = mode
	return d
}
func (d fixtureDecoder) Feedback(emit func(player.Feedback)) io.Writer {
	return feedback.NewWriter(func(line string) (player.Feedback, bool) {
		return player.Feedback{Kind: player.FeedbackPosition, Position: 12.5}, line == "FRAME 12.5"
	}, emit)
}

func TestInjectedDecoderLaunchesAndOwnsFeedback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decoder")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'FRAME 12.5\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	config := Config{VideoDecoder: fixtureDecoder{Decoder: ffplay.Decoder{Player: path}}}
	item := media.Item{Type: "Movie"}
	d, executable, err := resolveDecoder(config, item, PictureZoom43)
	if err != nil {
		t.Fatal(err)
	}
	if configured := config.VideoDecoder.(fixtureDecoder); configured.Picture != PictureOriginal {
		t.Fatal("request mutated shared decoder")
	}
	if configured, ok := d.(fixtureDecoder); !ok || configured.Picture != PictureZoom43 || d.Name() != "fixture" {
		t.Fatal("injected implementation was replaced")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	process, err := startProcess(ctx, executable, d.Args(item, ""), &mediaSource{}, d)
	if err != nil {
		t.Fatal(err)
	}
	process.feed()
	defer process.close()
	if err := <-process.done; err != nil {
		t.Fatal(err)
	}
	if len(process.positions) != 1 || <-process.positions != 12.5 {
		t.Fatal("playback did not consume the injected decoder's feedback")
	}
}
