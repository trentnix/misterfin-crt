package playback

import (
	"context"
	"errors"
	"time"

	"misterfin-go/internal/jellyfin"
)

// playbackSession owns one Jellyfin play session. Only its playback loop
// updates progress. Final reporting may continue after the decoder handoff.
type playbackSession struct {
	client                     *jellyfin.Client
	item                       jellyfin.Item
	start                      int64
	streamURL                  string
	live                       jellyfin.LivePlayback
	liveTV                     bool
	state                      jellyfin.PlayState
	played, started, reportErr bool
}

// closeLive releases a negotiated tuner with a fresh bounded context, even
// after the playback context has been canceled. Non-live sessions need no release.
func (s *playbackSession) closeLive() {
	if !s.liveTV {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.client.CloseLive(ctx, s.live.LiveStreamID)
}

// finish releases the server transcode even if the decoder never started.
// Copy state before asynchronous reporting so ownership is explicit.
func (s *playbackSession) finish(async <-chan struct{}, failed bool) {
	final := *s
	if final.liveTV {
		final.state.Failed = &failed
	}
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = final.client.ReportPlaying(ctx, "stopped", final.state)
		if final.started && !final.liveTV {
			_ = final.client.SavePlaybackPosition(ctx, final.item.ID, final.state.PositionTicks, final.played)
		}
	}
	select {
	case <-async:
		go cleanup()
	default:
		cleanup()
	}
}

func (s *playbackSession) update(ctx context.Context, seconds float64, position func(int64), startup *time.Timer) {
	if s.state.IsPaused && s.started {
		return
	}
	s.state.PositionTicks = s.start + int64(seconds*10000000)
	if !s.liveTV && s.item.RunTimeTicks > 0 {
		s.state.PositionTicks = min(s.state.PositionTicks, s.item.RunTimeTicks)
	}
	position(s.state.PositionTicks)
	if !s.started {
		s.started = true
		startup.Stop()
		s.reportErr = s.client.ReportPlaying(ctx, "start", s.state) != nil
		s.reportErr = s.client.ReportPlaying(ctx, "progress", s.state) != nil || s.reportErr
	}
}

// report publishes progress only after playback starts. When save is true,
// recorded media also persists its resume position. Reporting errors accumulate
// for the final result without interrupting otherwise successful decoding.
func (s *playbackSession) report(ctx context.Context, save bool) {
	if !s.started {
		return
	}
	s.reportErr = s.client.ReportPlaying(ctx, "progress", s.state) != nil || s.reportErr
	if save && !s.liveTV {
		s.reportErr = s.client.SavePlaybackPosition(ctx, s.item.ID, s.state.PositionTicks, s.played) != nil || s.reportErr
	}
}

func (s *playbackSession) control(ctx context.Context, p *playerProcess, o Options, control Control, startup *time.Timer) {
	switch control.Kind {
	case "pause":
		paused := !s.state.IsPaused
		if p.pause(o, paused) != nil {
			return
		}
		s.state.IsPaused = paused
		if !s.started {
			if paused {
				startup.Stop()
			} else {
				startup.Reset(30 * time.Second)
			}
		}
		if o.Paused != nil {
			o.Paused(paused)
		}
		s.report(ctx, false)
	case "refresh":
		if !o.Headless && s.state.IsPaused {
			p.refresh()
		}
	}
}

// monitor consumes process completion exactly once. Cancellation and startup
// timeout terminate and reap the decoder before returning to Run's cleanup.
func (s *playbackSession) monitor(ctx, mediaCtx context.Context, cancel context.CancelFunc, p *playerProcess, o Options, position func(int64)) error {
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	report := time.NewTicker(10 * time.Second)
	defer report.Stop()
	startup := time.NewTimer(30 * time.Second)
	defer startup.Stop()
	controls := o.Controls
	for {
		select {
		case control, ok := <-controls:
			if !ok {
				controls = nil
				continue
			}
			s.control(mediaCtx, p, o, control, startup)
		case waiting := <-p.buffering:
			if o.Buffering != nil {
				o.Buffering(waiting)
			}
		case seconds := <-p.positions:
			s.update(mediaCtx, seconds, position, startup)
		case <-poll.C:
			if !o.Headless {
				p.poll()
			}
		case <-report.C:
			s.report(mediaCtx, true)
		case <-startup.C:
			cancel()
			<-p.done
			return errors.New("player did not start playback within 30 seconds")
		case <-ctx.Done():
			cancel()
			<-p.done
			return nil
		case err := <-p.done:
			// Reap first, then apply all progress already parsed from the final output.
			s.drain(mediaCtx, p, position, startup)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil || !s.started {
				return errors.New("player could not play the stream")
			}
			s.played = s.played || !s.liveTV && s.item.RunTimeTicks > 0 && s.state.PositionTicks >= s.item.RunTimeTicks-2*10000000
			if s.reportErr {
				return errors.New("playback ended, but Jellyfin progress reporting failed")
			}
			return nil
		}
	}
}

func (s *playbackSession) drain(ctx context.Context, p *playerProcess, position func(int64), startup *time.Timer) {
	for {
		select {
		case seconds := <-p.positions:
			s.update(ctx, seconds, position, startup)
		default:
			return
		}
	}
}
