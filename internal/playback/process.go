package playback

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// playerProcess owns decoder pipes and progress channels. Wait runs exactly
// once. The playback loop consumes done before output ownership is released.
type playerProcess struct {
	decoder      decoder
	control      decoderControl
	cmd          *exec.Cmd
	commands     io.WriteCloser
	writer       *os.File
	source       *mediaSource
	positions    chan float64
	levels       chan AudioLevels
	buffering    chan bool
	videoStarted chan struct{}
	pictures     chan PictureResult
	done         chan error
	copyDone     chan struct{}
}

// startProcess starts the decoder with isolated process-group control and a
// stream on file descriptor 3. It closes its pipes on failure. On success the
// caller must call feed, consume done, and then close, in that order.
func startProcess(ctx context.Context, executable string, args []string, source *mediaSource, decoder decoder) (*playerProcess, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, errors.New("cannot open player stream pipe")
	}
	defer reader.Close()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Resume a stopped player so it can handle termination and restore video.
	cmd.Cancel = func() error {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGCONT)
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 2 * time.Second
	cmd.ExtraFiles = []*os.File{reader}
	p := &playerProcess{decoder: decoder, cmd: cmd, writer: writer, source: source, positions: make(chan float64, 16), buffering: make(chan bool, 16), done: make(chan error, 1), copyDone: make(chan struct{})}
	p.videoStarted = make(chan struct{}, 1)
	p.levels = make(chan AudioLevels, 1)
	p.pictures = make(chan PictureResult, 16)
	output := &positionWriter{levels: p.levels, positions: p.positions, buffering: p.buffering, videoStarted: p.videoStarted, pictures: p.pictures}
	cmd.Stdout, cmd.Stderr = output, output
	p.commands, err = cmd.StdinPipe()
	if err != nil {
		writer.Close()
		return nil, errors.New("cannot open player control pipe")
	}
	p.control = decoderControl{
		stdin: p.commands,
		signal: func(signal syscall.Signal) error {
			return syscall.Kill(-cmd.Process.Pid, signal)
		},
	}
	if err = cmd.Start(); err != nil {
		p.commands.Close()
		writer.Close()
		return nil, errors.New("cannot start media player")
	}
	return p, nil
}

// feed starts after AcquireVideo, preserving the framebuffer handoff order.
func (p *playerProcess) feed() {
	go func() {
		if p.source.stream != nil {
			_, _ = io.Copy(p.writer, p.source.stream)
		}
		p.writer.Close()
		close(p.copyDone)
	}()
	go func() { p.done <- p.cmd.Wait() }()
}

// close follows process completion. Closing the stream unblocks a copier that
// is still waiting for data after the decoder exits.
func (p *playerProcess) close() {
	if p.source.stream != nil {
		p.source.stream.Close()
	}
	p.writer.Close()
	<-p.copyDone
	p.commands.Close()
}

// pause sends the selected decoder's command. Success means the transport
// accepted the request, not that the decoder has acknowledged its new state.
func (p *playerProcess) pause(paused bool) error {
	return p.decoder.pause(p.control, paused)
}

func (p *playerProcess) poll()    { p.decoder.poll(p.control) }
func (p *playerProcess) refresh() { p.decoder.refresh(p.control) }
