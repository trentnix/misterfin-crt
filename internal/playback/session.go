package playback

import (
	"context"
	"errors"
	"time"

	"misterfin-go/internal/jellyfin"
)

// playbackSession owns one Jellyfin play session. Its loop updates decoder
// state and queues snapshots to progressReporter without waiting for HTTP.
type playbackSession struct {
	client          *jellyfin.Client
	item            jellyfin.Item
	start           int64
	streamURL       string
	live            jellyfin.LivePlayback
	liveTV          bool
	state           jellyfin.PlayState
	played, started bool
	reporter        *progressReporter
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

func (s *playbackSession) update(seconds float64, position func(int64), startup *time.Timer) {
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
		s.reporter.start(s.state)
	}
}

// report queues the latest state after playback starts. Periodic reports also
// save recorded media's resume position. Queueing cannot wait for HTTP.
func (s *playbackSession) report(save bool) {
	if s.started {
		s.reporter.progress(s.state, save, s.played)
	}
}

func (s *playbackSession) control(p *playerProcess, o Options, control Control, startup *time.Timer) {
	switch control.Kind {
	case "pause":
		paused := !s.state.IsPaused
		if p.pause(paused) != nil {
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
		s.report(false)
	case "refresh":
		if s.state.IsPaused {
			p.refresh()
		}
	}
}

// monitor consumes process completion exactly once. Cancellation and startup
// timeout terminate and reap the decoder before returning to Run's cleanup.
func (s *playbackSession) monitor(ctx context.Context, cancel context.CancelFunc, p *playerProcess, o Options, position func(int64)) error {
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	report := time.NewTicker(10 * time.Second)
	defer report.Stop()
	startup := time.NewTimer(30 * time.Second)
	defer startup.Stop()
	controls := o.Controls
	videoStarted := p.videoStarted
	for {
		select {
		case <-videoStarted:
			videoStarted = nil
			if o.VideoStarted != nil {
				o.VideoStarted()
			}
		case control, ok := <-controls:
			if !ok {
				controls = nil
				continue
			}
			s.control(p, o, control, startup)
		case waiting := <-p.buffering:
			if o.Buffering != nil {
				o.Buffering(waiting)
			}
		case seconds := <-p.positions:
			s.update(seconds, position, startup)
		case <-poll.C:
			p.poll()
		case <-report.C:
			s.report(true)
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
			s.drain(p, position, startup)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil || !s.started {
				return errors.New("player could not play the stream")
			}
			s.played = s.played || !s.liveTV && s.item.RunTimeTicks > 0 && s.state.PositionTicks >= s.item.RunTimeTicks-2*10000000
			return nil
		}
	}
}

func (s *playbackSession) drain(p *playerProcess, position func(int64), startup *time.Timer) {
	for {
		select {
		case seconds := <-p.positions:
			s.update(seconds, position, startup)
		default:
			return
		}
	}
}
