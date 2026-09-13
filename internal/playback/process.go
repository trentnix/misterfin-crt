package playback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// playerProcess owns decoder pipes and progress channels. Wait runs exactly
// once. The playback loop consumes done before output ownership is released.
type playerProcess struct {
	cmd       *exec.Cmd
	commands  io.WriteCloser
	writer    *os.File
	source    *mediaSource
	positions chan float64
	buffering chan bool
	done      chan error
	copyDone  chan struct{}
}

// startProcess starts the decoder with isolated process-group control and a
// stream on file descriptor 3. It closes its pipes on failure. On success the
// caller must call feed, consume done, and then close, in that order.
func startProcess(ctx context.Context, executable string, args []string, source *mediaSource) (*playerProcess, error) {
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
	p := &playerProcess{cmd: cmd, writer: writer, source: source, positions: make(chan float64, 16), buffering: make(chan bool, 16), done: make(chan error, 1), copyDone: make(chan struct{})}
	output := &positionWriter{positions: p.positions, buffering: p.buffering}
	cmd.Stdout, cmd.Stderr = output, output
	p.commands, err = cmd.StdinPipe()
	if err != nil {
		writer.Close()
		return nil, errors.New("cannot open player control pipe")
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

func (p *playerProcess) pause(o Options, paused bool) error {
	if o.TerminalPlayer != "" {
		_, err := fmt.Fprintf(p.commands, "pause %t\n", paused)
		return err
	}
	if !o.Headless {
		_, err := io.WriteString(p.commands, "pause\n")
		return err
	}
	signal := syscall.SIGSTOP
	if !paused {
		signal = syscall.SIGCONT
	}
	return syscall.Kill(-p.cmd.Process.Pid, signal)
}

func (p *playerProcess) poll() {
	_, _ = io.WriteString(p.commands, "pausing_keep_force get_time_pos\n")
}
func (p *playerProcess) refresh() {
	_, _ = io.WriteString(p.commands, "pausing_keep_force osd_show_text \" \" 1\n")
}
