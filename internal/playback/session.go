package playback

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"misterfin-crt/internal/jellyfin"
	playerapi "misterfin-crt/internal/player"
)

// playbackSession owns one Jellyfin play session. Its loop updates decoder
// state and queues snapshots to progressReporter without waiting for HTTP.
type playbackSession struct {
	trace           *playbackTrace
	meter           playerapi.Meter // Borrowed from Run, which closes it after decoder cleanup.
	tracks          VideoTracks
	client          *jellyfin.Client
	item            jellyfin.Item
	start           int64
	streamURL       string
	live            jellyfin.LivePlayback
	liveTV          bool
	state           jellyfin.PlayState
	played, started bool
	reporter        *progressReporter
	preferences     *Preferences
	preferenceKey   string
}

// rememberChoices saves only choices used by a running recorded video.
// Preparation failures and canceled replacements must not replace saved choices.
func (s *playbackSession) rememberChoices() {
	if s.started && !s.liveTV && s.item.Type != "Audio" {
		s.preferences.save(s.preferenceKey, s.tracks)
	}
}

// finish follows decoder, output, and source cleanup. Stop/save and tuner
// release remain ordered, but an explicit handoff lets them outlive Run.
func (s *playbackSession) finish(failed bool, async <-chan struct{}, cleanupDone func()) bool {
	reportFailed := s.reporter.finish(s.state, s.started, s.played, failed, async)
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-s.reporter.done
		s.closeLive()
		if cleanupDone != nil {
			cleanupDone()
		}
	}()
	select {
	case <-async:
	case <-done:
	}
	return reportFailed
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

// update converts decoder seconds to an item position using the session offset.
// The first accepted position ends the startup timeout and starts reporting.
// Once video starts, paused feedback cannot advance its saved position.
func (s *playbackSession) update(seconds float64, position func(int64), startup *time.Timer) {
	if s.state.IsPaused && s.started && s.item.Type != "Audio" {
		return
	}
	s.state.PositionTicks = s.start + int64(seconds*10000000)
	if !s.liveTV && s.item.RunTimeTicks > 0 {
		s.state.PositionTicks = min(s.state.PositionTicks, s.item.RunTimeTicks)
	}
	if position != nil {
		position(s.state.PositionTicks)
	}
	if !s.started {
		s.started = true
		s.trace.record("playback.first-position", slog.Int64("position_ticks", s.state.PositionTicks))
		startup.Stop()
		s.reporter.start(s.state)
		s.rememberChoices()
	}
}

// report queues the latest state after playback starts. Periodic reports also
// save recorded media's resume position. Queueing cannot wait for HTTP.
func (s *playbackSession) report(save bool) {
	if s.started {
		s.reporter.progress(s.state, save, s.played)
	}
}

// control dispatches a playback command on the session loop. Pause state
// changes after command delivery. Picture state waits for player acknowledgment.
// Seeking here applies only to audio. Video seeks replace the session.
func (s *playbackSession) control(p *playerProcess, callbacks Callbacks, control Control, startup *time.Timer) {
	switch control.Kind {
	case "picture":
		setter, ok := p.decoder.(playerapi.PictureSetter)
		if !ok || s.item.Type == "Audio" || control.Picture > PictureZoom43 || setter.SetPicture(p.control, control.Picture, control.Request) != nil {
			if callbacks.Picture != nil {
				callbacks.Picture(PictureResult{Request: control.Request, Err: errors.New("cannot change picture mode")})
			}
		}
	case "report":
		if s.started {
			s.report(false)
		}
	case "pause", "set-pause", "resume":
		paused := !s.state.IsPaused
		if control.Kind != "pause" {
			paused = control.Kind == "set-pause"
		}
		if paused == s.state.IsPaused {
			return
		}
		if p.pause(paused) != nil {
			return
		}
		s.state.IsPaused = paused
		s.trace.record("playback.pause", slog.Bool("paused", paused))
		if !s.started {
			if paused {
				startup.Stop()
			} else {
				startup.Reset(30 * time.Second)
			}
		}
		if callbacks.Paused != nil {
			callbacks.Paused(paused)
		}
		s.report(false)
	case "seek", "seek-to":
		if s.item.Type != "Audio" || !s.started || (control.Kind == "seek" && control.Seconds != -10 && control.Seconds != 10) {
			return
		}
		seeker, ok := p.decoder.(playerapi.AudioSeeker)
		var err error
		if !ok {
			err = errors.New("music seeking requires the audio helper or MPlayer")
		} else if seeker.Seek(p.control, control.Seconds) != nil {
			err = errors.New("cannot seek music")
		} else {
			p.poll()
		}
		if err != nil && callbacks.ControlError != nil {
			callbacks.ControlError(err)
		}
	case "refresh":
		if s.state.IsPaused {
			p.refresh()
		}
	}
}

// monitor consumes process completion exactly once. Cancellation and startup
// timeout terminate and reap the decoder before returning to Run's cleanup.
func (s *playbackSession) monitor(ctx context.Context, cancel context.CancelFunc, p *playerProcess, request Request) error {
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	report := time.NewTicker(10 * time.Second)
	defer report.Stop()
	startup := time.NewTimer(30 * time.Second)
	defer startup.Stop()
	var audioTimer *time.Ticker
	var audioTick <-chan time.Time
	if s.meter != nil {
		audioTimer = time.NewTicker(50 * time.Millisecond)
		audioTick = audioTimer.C
		defer audioTimer.Stop()
	}
	loader := subtitleLoader{results: make(chan SubtitleResult, 1)}
	defer loader.stop()
	if s.tracks.ClientSubtitles && s.tracks.Text == nil {
		if sub, ok := s.tracks.Stream("Subtitle", s.tracks.Selection.SubtitleIndex); ok && sub.TextSubtitle() {
			loader.start(ctx, s.client, s.item.ID, s.tracks.SourceID, sub.Index, 0)
		}
	}
	controls := request.Controls
	videoStarted := p.videoStarted
	for {
		select {
		case text := <-p.captions:
			if s.liveTV && request.Callbacks.Caption != nil {
				request.Callbacks.Caption(text)
			}
		case result := <-p.pictures:
			if result.Err == nil {
				s.tracks.Picture = result.Mode
				s.rememberChoices()
			}
			if request.Callbacks.Picture != nil {
				request.Callbacks.Picture(result)
			}
		case result := <-loader.results:
			if result.serial != loader.serial {
				continue
			}
			if result.Err == nil {
				s.tracks.Selection.SubtitleIndex = result.Index
				s.tracks.Text = result.Text
				index := result.Index
				s.state.SubtitleStreamIndex = &index
				s.report(false)
				s.rememberChoices()
			}
			if request.Callbacks.Subtitle != nil {
				request.Callbacks.Subtitle(result)
			}
		case <-audioTick:
			levels := AudioLevels{}
			if !s.state.IsPaused {
				levels = s.meter.Levels()
			}
			if request.Callbacks.Levels != nil {
				request.Callbacks.Levels(levels)
			}
		case levels := <-p.levels:
			if s.state.IsPaused {
				levels = AudioLevels{}
			}
			if request.Callbacks.Levels != nil {
				request.Callbacks.Levels(levels)
			}
		case <-videoStarted:
			videoStarted = nil
			s.trace.record("playback.first-frame")
			if request.Callbacks.VideoStarted != nil {
				request.Callbacks.VideoStarted()
			}
		case control, ok := <-controls:
			if !ok {
				controls = nil
				continue
			}
			if control.Kind == "subtitle" {
				sub, ok := s.tracks.Stream("Subtitle", control.Index)
				if s.tracks.ClientSubtitles && (control.Index == -1 || ok && sub.TextSubtitle()) {
					loader.start(ctx, s.client, s.item.ID, s.tracks.SourceID, control.Index, control.Request)
				}
			} else {
				s.control(p, request.Callbacks, control, startup)
			}
		case waiting := <-p.buffering:
			s.trace.buffering(waiting)
			if request.Callbacks.Buffering != nil {
				request.Callbacks.Buffering(waiting)
			}
		case seconds := <-p.positions:
			s.update(seconds, request.Callbacks.Position, startup)
		case <-poll.C:
			p.poll()
		case <-report.C:
			s.trace.record("playback.progress", slog.Int64("position_ticks", s.state.PositionTicks), slog.Bool("paused", s.state.IsPaused))
			s.report(true)
		case <-startup.C:
			s.trace.record("playback.startup-timeout")
			cancel()
			s.trace.decoderExit(<-p.done)
			return errors.New("player did not start playback within 30 seconds")
		case <-ctx.Done():
			cancel()
			s.trace.decoderExit(<-p.done)
			return nil
		case err := <-p.done:
			s.trace.decoderExit(err)
			// Reap first, then apply all progress already parsed from the final output.
			s.drain(p, request.Callbacks.Position, startup)
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

// drain applies queued positions after the decoder has been reaped so the
// final report includes progress received immediately before process exit.
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
