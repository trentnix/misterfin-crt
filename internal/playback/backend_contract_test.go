package playback

import (
	"context"
	"errors"
	"testing"
	"time"

	"misterfin-crt/internal/media"
)

// timedOutReports forces the final report to consume its entire cleanup budget.
type timedOutReports struct{ final context.Context }

func (r *timedOutReports) ReportPlaying(ctx context.Context, event string, _ media.PlayState) error {
	if event != "stopped" {
		return nil
	}
	r.final = ctx
	<-ctx.Done()
	return ctx.Err()
}
func (*timedOutReports) SavePlaybackPosition(context.Context, string, int64, bool) error { return nil }

func TestStreamReleaseSurvivesFinalReportTimeout(t *testing.T) {
	reports := &timedOutReports{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	released := 0
	cleanup := make(chan struct{})
	s := playbackSession{reporter: newProgressReporter(ctx, reports, false), stream: media.PreparedStream{
		Reports: reports,
		Release: func(ctx context.Context) error {
			released++
			if !errors.Is(reports.final.Err(), context.DeadlineExceeded) {
				t.Error("final report did not time out")
			}
			if ctx.Err() != nil || ctx == reports.final {
				t.Error("release inherited the exhausted report context")
			}
			if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) < 4*time.Second {
				t.Error("release did not receive its own bounded budget")
			}
			return nil
		},
	}}
	fast := make(chan struct{})
	close(fast)
	s.finish(true, fast, func() { close(cleanup) })
	select {
	case <-cleanup:
	case <-time.After(7 * time.Second):
		t.Fatal("detached cleanup did not complete")
	}
	if released != 1 {
		t.Fatalf("released %d times", released)
	}
}
