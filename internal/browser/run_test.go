package browser

import (
	"context"
	"errors"
	"testing"
	"time"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/playback"
	"misterfin-crt/internal/rendering"
	"misterfin-crt/internal/videoout"
)

func TestRunBorrowsInputAndOutput(t *testing.T) {
	for _, mode := range []string{"quit", "closed", "canceled", "display error"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			keys := make(chan control.Event, 1)
			output := &runTestOutput{}
			var want string
			switch mode {
			case "quit":
				keys <- control.Event{Action: control.Quit, Labels: control.KeyboardLabels()}
			case "closed":
				close(keys)
				want = "input closed"
			case "canceled":
				cancel()
			case "display error":
				output.err = errors.New("display failed")
				want = output.err.Error()
			}
			// No terminal or framebuffer is opened. Authentication cannot reach a server.
			dir := t.TempDir()
			config := Config{StateDir: dir}
			err := Run(ctx, config, playback.Config{}, output, rendering.NewRenderer(), nil, keys)
			if (want == "" && err != nil) || (want != "" && (err == nil || err.Error() != want)) {
				t.Fatalf("got %v, want %q", err, want)
			}
			if !output.cleared || output.closed {
				t.Fatal("browser must clear output without closing its caller's resource")
			}
			if mode != "canceled" && ctx.Err() != nil {
				t.Fatalf("browser canceled caller context or failed to exit: %v", ctx.Err())
			}
			if mode == "quit" {
				// Run must neither close nor retain ownership of the caller's channel.
				keys <- control.Event{Action: control.Quit}
				close(keys)
			}
		})
	}
}

type runTestOutput struct {
	videoout.Output
	cleared, closed bool
	err             error
}

func (*runTestOutput) Geometry() platform.Geometry {
	return platform.Geometry{Width: 640, Height: 240, OutputWidth: 640, OutputHeight: 240}
}
func (*runTestOutput) FrameInterval(bool) time.Duration { return time.Second / 60 }
func (o *runTestOutput) Present(videoout.Frame) error   { return o.err }
func (o *runTestOutput) Clear()                         { o.cleared = true }
func (o *runTestOutput) Close() error                   { o.closed = true; return nil }
