// Package playback runs an external video player and owns its Jellyfin session.
package playback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"misterfin-go/internal/jellyfin"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Options struct {
	Player         string
	TerminalPlayer string
	FrameOutput    string
	Headless       bool
	Device         string
	Width, Height  int
}

func Supported(item jellyfin.Item) bool {
	if jellyfin.IsLive(item) {
		return true
	}
	switch item.Type {
	case "Movie", "Episode", "Video", "MusicVideo", "Audio":
		return true
	}
	return false
}
func (o Options) executable() string {
	if o.Player != "" {
		return o.Player
	}
	if o.TerminalPlayer != "" {
		return "python3"
	}
	if o.Headless {
		return "ffplay"
	}
	return "/media/fat/misterfin/mplayer-arm"
}
func (o Options) args(item jellyfin.Item) []string {
	if item.Type == "Audio" {
		if o.TerminalPlayer != "" {
			return []string{o.TerminalPlayer, "--audio-only"}
		}
		if o.Headless {
			return []string{"-hide_banner", "-loglevel", "info", "-stats", "-autoexit", "-nodisp", "-vn", "-af", "asetpts=PTS-STARTPTS", "-i", "pipe:3"}
		}
		return []string{"-slave", "-quiet", "-nojoystick", "-noconsolecontrols", "-novideo", "-ao", "alsa", "-af", "volume=-3,lavcresample=48000", "/dev/fd/3"}
	}
	if o.TerminalPlayer != "" {
		return []string{o.TerminalPlayer, "--output", o.FrameOutput, "--width", strconv.Itoa(o.Width), "--height", strconv.Itoa(o.Height)}
	}
	if o.Headless {
		return []string{"-hide_banner", "-loglevel", "info", "-stats", "-autoexit", "-exitonkeydown", "-window_title", "MiSTerFin-Go playback", "-vf", "setpts=PTS-STARTPTS", "-af", "asetpts=PTS-STARTPTS", "-i", "pipe:3"}
	}
	dar := 4.0 / 3
	for _, stream := range item.MediaStreams {
		if stream.Type == "Video" {
			if stream.Width > 0 && stream.Height > 0 {
				dar = float64(stream.Width) / float64(stream.Height)
			}
			parts := strings.Split(stream.AspectRatio, ":")
			if len(parts) == 2 {
				a, e1 := strconv.ParseFloat(parts[0], 64)
				b, e2 := strconv.ParseFloat(parts[1], 64)
				if e1 == nil && e2 == nil && a > 0 && b > 0 {
					dar = a / b
				}
			}
			break
		}
	}
	if math.IsNaN(dar) || math.IsInf(dar, 0) || dar < 0.1 || dar > 10 {
		dar = 4.0 / 3
	}
	par := float64(o.Width) * 3 / float64(o.Height*4)
	w := o.Width
	h := int(float64(w)/(dar*par) + 0.5)
	if h > o.Height {
		h = o.Height
		w = int(float64(h)*dar*par + 0.5)
	}
	filter := fmt.Sprintf("scale=%d:%d,expand=%d:%d,dsize=%d:%d", max(2, w/2*2), max(2, h/2*2), o.Width, o.Height, o.Width, o.Height)
	return []string{"-slave", "-quiet", "-nojoystick", "-noconsolecontrols", "-vo", "fbdev:" + o.Device, "-ao", "alsa", "-osdlevel", "0", "-demuxer", "lavf", "-cache", "8192", "-cache-min", "20", "-sws", "0", "-vf", filter, "-lavdopts", "threads=2:fast", "-af", "volume=-3", "/dev/fd/3"}
}

// Run never draws into the framebuffer. The caller must stop presenting frames
// until Run returns. Cancel stops the player, closes the stream, and reaps it.
func Run(ctx context.Context, c *jellyfin.Client, item jellyfin.Item, o Options, position func(int64)) (resultErr error) {
	if !Supported(item) {
		return errors.New("playback for this item type is not implemented")
	}
	if !o.Headless && (o.Width != 640 || (o.Height != 240 && o.Height != 288 && o.Height != 480 && o.Height != 576)) {
		return errors.New("MiSTer playback currently requires a 640-pixel PAL or NTSC framebuffer")
	}
	if o.TerminalPlayer != "" && (!o.Headless || o.FrameOutput == "" || o.Player != "" || o.Width != 640 || (o.Height != 240 && o.Height != 288)) {
		return errors.New("terminal playback requires 640x240 or 640x288 headless output and no player override")
	}
	executable, err := exec.LookPath(o.executable())
	if err != nil {
		return fmt.Errorf("player not found: %s", o.executable())
	}
	liveTV := jellyfin.IsLive(item)
	item, err = c.Details(ctx, item.ID)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return errors.New("cannot load playback details")
	}
	if liveTV {
		item.Type = "TvChannel"
	}
	liveTV = jellyfin.IsLive(item)
	if !Supported(item) {
		return errors.New("playback for this item type is not implemented")
	}
	session, err := jellyfin.NewPlaySessionID()
	if err != nil {
		return errors.New("cannot create playback session")
	}
	start := max(int64(0), item.UserData.PlaybackPositionTicks)
	if item.UserData.Played || liveTV || item.Type == "Audio" {
		start = 0
	}
	streamURL := c.VideoStreamURL(item.ID, session, start, o.Height == 240 || o.Height == 480)
	if item.Type == "Audio" {
		streamURL = c.AudioStreamURL(item.ID, session)
	}
	var live jellyfin.LivePlayback
	if liveTV {
		live, err = c.OpenLive(ctx, item.ID, o.Height == 240 || o.Height == 480)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		session, streamURL = live.PlaySessionID, live.StreamURL
		defer func() {
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_ = c.CloseLive(cleanup, live.LiveStreamID)
		}()
	}
	state := jellyfin.PlayState{ItemID: item.ID, PlaySessionID: session, PositionTicks: start}
	if item.Type == "Audio" {
		state.PlayMethod = "DirectStream"
	}
	if liveTV {
		canSeek := false
		state.MediaSourceID, state.LiveStreamID, state.CanSeek = live.MediaSourceID, live.LiveStreamID, &canSeek
	}
	mediaCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Even a player that cannot start must release the server's transcode.
	played := item.UserData.Played
	started := false
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if liveTV {
			failed := resultErr != nil
			state.Failed = &failed
		}
		_ = c.ReportPlaying(cleanup, "stopped", state)
		if started && !liveTV {
			_ = c.SavePlaybackPosition(cleanup, item.ID, state.PositionTicks, played)
		}
	}()
	stream, err := c.OpenStream(mediaCtx, streamURL)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer stream.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		return errors.New("cannot open player stream pipe")
	}
	defer reader.Close()
	defer writer.Close()
	cmd := exec.CommandContext(mediaCtx, executable, o.args(item)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Give the player a chance to restore its video device before the
	// CommandContext watchdog falls back to killing an unresponsive process.
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second
	cmd.ExtraFiles = []*os.File{reader}
	updates := make(chan float64, 16)
	output := &positionWriter{positions: updates}
	cmd.Stdout = output
	cmd.Stderr = output
	commands, err := cmd.StdinPipe()
	if err != nil {
		return errors.New("cannot open player control pipe")
	}
	defer commands.Close()
	if err = cmd.Start(); err != nil {
		return errors.New("cannot start media player")
	}
	reader.Close()
	copyDone := make(chan struct{})
	go func() { _, _ = io.Copy(writer, stream); writer.Close(); close(copyDone) }()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() { cancel(); stream.Close(); writer.Close(); <-copyDone }()
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	report := time.NewTicker(10 * time.Second)
	defer report.Stop()
	startup := time.NewTimer(30 * time.Second)
	defer startup.Stop()
	reportErr := false
	update := func(seconds float64) {
		state.PositionTicks = start + int64(seconds*10000000)
		if !liveTV && item.RunTimeTicks > 0 {
			state.PositionTicks = min(state.PositionTicks, item.RunTimeTicks)
		}
		position(state.PositionTicks)
		if !started {
			started = true
			startup.Stop()
			reportErr = c.ReportPlaying(mediaCtx, "start", state) != nil
			reportErr = c.ReportPlaying(mediaCtx, "progress", state) != nil || reportErr
		}
	}
	for {
		select {
		case seconds := <-updates:
			update(seconds)
		case <-poll.C:
			if !o.Headless {
				_, _ = io.WriteString(commands, "pausing_keep_force get_time_pos\n")
			}
		case <-report.C:
			if started {
				reportErr = c.ReportPlaying(mediaCtx, "progress", state) != nil || reportErr
				if !liveTV {
					reportErr = c.SavePlaybackPosition(mediaCtx, item.ID, state.PositionTicks, played) != nil || reportErr
				}
			}
		case <-startup.C:
			cancel()
			<-done
			return errors.New("player did not start playback within 30 seconds")
		case <-ctx.Done():
			cancel()
			<-done
			return nil
		case err := <-done:
			for {
				select {
				case seconds := <-updates:
					update(seconds)
				default:
					goto drained
				}
			}
		drained:
			if ctx.Err() != nil {
				return nil
			}
			if err != nil || !started {
				return errors.New("player could not play the stream")
			}
			played = played || !liveTV && item.RunTimeTicks > 0 && state.PositionTicks >= item.RunTimeTicks-2*10000000
			if reportErr {
				return errors.New("playback ended, but Jellyfin progress reporting failed")
			}
			return nil
		}
	}
}

// Parse only numeric progress. Player diagnostics may contain media URLs and
// must never be copied to terminal output or error messages.
type positionWriter struct {
	mu        sync.Mutex
	pending   string
	positions chan float64
}

func (p *positionWriter) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, b := range data {
		if b == '\r' || b == '\n' {
			line := strings.TrimSpace(p.pending)
			p.pending = ""
			value := ""
			if strings.HasPrefix(line, "ANS_TIME_POSITION=") {
				value = strings.TrimPrefix(line, "ANS_TIME_POSITION=")
			} else if fields := strings.Fields(line); len(fields) > 1 && (fields[1] == "A-V:" || fields[1] == "M-V:" || fields[1] == "M-A:") {
				value = fields[0]
			}
			if seconds, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) && seconds >= 0 && seconds < 1e9 {
				select {
				case p.positions <- seconds:
				default:
				}
			}
		} else if len(p.pending) < 8192 {
			p.pending += string(b)
		}
	}
	return len(data), nil
}
