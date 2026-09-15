package playback

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"misterfin-crt/internal/diagnostics"
)

var diagnosticSequence atomic.Uint64

// playbackTrace records session milestones on the playback loop. IDs are local
// counters, not Jellyfin session or item identifiers. Nil disables all work.
type playbackTrace struct {
	log                     *diagnostics.Log
	id                      uint64
	start                   time.Time
	stage                   string
	bufferingKnown, waiting bool
}

func newPlaybackTrace(log *diagnostics.Log, config Config, request Request) *playbackTrace {
	if log == nil {
		return nil
	}
	t := &playbackTrace{log: log, id: diagnosticSequence.Add(1), start: time.Now()}
	decoder := config.VideoDecoder.Kind
	if request.Item.Type == "Audio" {
		decoder = config.AudioDecoder.Kind
	}
	name := "unknown"
	switch decoder {
	case DecoderMPlayer:
		name = "mplayer"
	case DecoderFFplay:
		name = "ffplay"
	case DecoderPython:
		name = "python"
	}
	t.record("playback.start", slog.String("decoder", name), slog.Bool("audio", request.Item.Type == "Audio"))
	return t
}

func (t *playbackTrace) record(event string, attrs ...slog.Attr) {
	if t == nil {
		return
	}
	attrs = append(attrs, slog.Uint64("playback", t.id), slog.Int64("elapsed_ms", time.Since(t.start).Milliseconds()))
	t.log.Record(event, attrs...)
}

// phase identifies where a failure or cancellation occurred, without retaining
// a raw error, command line, media title, or authenticated stream URL.
func (t *playbackTrace) phase(stage string) {
	if t == nil {
		return
	}
	t.stage = stage
	t.record("playback.phase", slog.String("stage", stage))
}

func (t *playbackTrace) finish(ctx context.Context, err error) {
	if t == nil {
		return
	}
	t.record("playback.end", slog.String("stage", t.stage), slog.Bool("failed", err != nil), slog.Bool("canceled", ctx.Err() != nil))
}

// prepared copies only numeric transcode limits from the private stream URL.
// Audio streams and absent parameters report zero. No arbitrary query data is logged.
func (t *playbackTrace) prepared(s *playbackSession) {
	if t == nil {
		return
	}
	attrs := []slog.Attr{slog.Int64("start_ticks", s.start), slog.Bool("live", s.liveTV)}
	if u, err := url.Parse(s.streamURL); err == nil {
		q := u.Query()
		for _, key := range []string{"maxWidth", "maxHeight", "videoBitRate", "maxFramerate"} {
			value := q.Get(key)
			if value == "" {
				for name, values := range q {
					if strings.EqualFold(name, key) && len(values) > 0 {
						value = values[0]
						break
					}
				}
			}
			n, _ := strconv.ParseFloat(value, 64)
			if n < 0 || n > 1e9 || n != n {
				n = 0
			}
			attrs = append(attrs, slog.Float64(key, n))
		}
	}
	t.record("playback.prepared", attrs...)
}

func (t *playbackTrace) buffering(waiting bool) {
	if t == nil || t.bufferingKnown && t.waiting == waiting {
		return
	}
	t.bufferingKnown, t.waiting = true, waiting
	t.record("playback.buffering", slog.Bool("waiting", waiting))
}

// decoderExit records numeric process status without retaining stderr or an
// exec error's command text. Signal numbers follow the host operating system.
func (t *playbackTrace) decoderExit(err error) {
	if t == nil {
		return
	}
	code, signal := 0, 0
	if err != nil {
		code = -1
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			signal = int(status.Signal())
		}
	}
	t.record("playback.decoder-exit", slog.Int("exit_code", code), slog.Int("signal", signal), slog.Bool("failed", err != nil))
}
