package playback

import (
	"context"
	"errors"
	"time"

	"misterfin-crt/internal/jellyfin"
)

// playbackSession owns one Jellyfin play session. Its loop updates decoder
// state and queues snapshots to progressReporter without waiting for HTTP.
type playbackSession struct {
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
func (s *playbackSession) finish(failed bool, o Options) bool {
	reportFailed := s.reporter.finish(s.state, s.started, s.played, failed, o.AsyncCleanup)
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-s.reporter.done
		s.closeLive()
		if o.CleanupDone != nil {
			o.CleanupDone()
		}
	}()
	select {
	case <-o.AsyncCleanup:
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

func (s *playbackSession) update(seconds float64, position func(int64), startup *time.Timer) {
	if s.state.IsPaused && s.started && s.item.Type != "Audio" {
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

func (s *playbackSession) control(p *playerProcess, o Options, control Control, startup *time.Timer) {
	switch control.Kind {
	case "picture":
		setter, ok := p.decoder.(pictureSetter)
		if !ok || s.liveTV || s.item.Type == "Audio" || control.Picture > PictureZoom43 || setter.setPicture(p.control, control.Picture, control.Request) != nil {
			if o.Picture != nil {
				o.Picture(PictureResult{Request: control.Request, Err: errors.New("cannot change picture mode")})
			}
		}
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
	case "seek":
		if s.item.Type != "Audio" || !s.started || (control.Seconds != -10 && control.Seconds != 10) {
			return
		}
		seeker, ok := p.decoder.(audioSeeker)
		var err error
		if !ok {
			err = errors.New("music seeking requires the audio helper or MPlayer")
		} else if seeker.seek(p.control, control.Seconds) != nil {
			err = errors.New("cannot seek music")
		} else {
			p.poll()
		}
		if err != nil && o.ControlError != nil {
			o.ControlError(err)
		}
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
	var audioTimer *time.Ticker
	var audioTick <-chan time.Time
	if o.audioExport != "" {
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
	controls := o.Controls
	videoStarted := p.videoStarted
	for {
		select {
		case result := <-p.pictures:
			if result.Err == nil {
				s.tracks.Picture = result.Mode
				s.rememberChoices()
			}
			if o.Picture != nil {
				o.Picture(result)
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
			if o.Subtitle != nil {
				o.Subtitle(result)
			}
		case <-audioTick:
			levels := AudioLevels{}
			if !s.state.IsPaused {
				levels = audioExport(o.audioExport)
			}
			if o.Levels != nil {
				o.Levels(levels)
			}
		case levels := <-p.levels:
			if s.state.IsPaused {
				levels = AudioLevels{}
			}
			if o.Levels != nil {
				o.Levels(levels)
			}
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
			if control.Kind == "subtitle" {
				sub, ok := s.tracks.Stream("Subtitle", control.Index)
				if s.tracks.ClientSubtitles && (control.Index == -1 || ok && sub.TextSubtitle()) {
					loader.start(ctx, s.client, s.item.ID, s.tracks.SourceID, control.Index, control.Request)
				}
			} else {
				s.control(p, o, control, startup)
			}
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
