package framefile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// frameWatch observes the directory because the decoder atomically replaces
// its frame file. A nonblocking descriptor lets os.File.Close interrupt Read.
type frameWatch struct {
	file    *os.File
	updates chan struct{}
	done    chan struct{}
}

func watchFrames(path string) (*frameWatch, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		return nil, fmt.Errorf("watch video frames: %w", err)
	}
	file := os.NewFile(uintptr(fd), "video frame notifications")
	if _, err := syscall.InotifyAddWatch(fd, filepath.Dir(path), syscall.IN_MOVED_TO|syscall.IN_CLOSE_WRITE); err != nil {
		file.Close()
		return nil, fmt.Errorf("watch video frame directory: %w", err)
	}
	w := &frameWatch{file: file, updates: make(chan struct{}, 1), done: make(chan struct{})}
	go w.read(filepath.Base(path))
	return w, nil
}

func (w *frameWatch) read(name string) {
	defer close(w.done)
	buffer := make([]byte, 4096)
	for {
		n, err := w.file.Read(buffer)
		if err != nil {
			return
		}
		for offset := 0; offset+16 <= n; {
			mask := binary.NativeEndian.Uint32(buffer[offset+4:])
			size := int(binary.NativeEndian.Uint32(buffer[offset+12:]))
			end := offset + 16 + size
			if end > n {
				break
			}
			filename := bytes.TrimRight(buffer[offset+16:end], "\x00")
			if string(filename) == name || mask&syscall.IN_Q_OVERFLOW != 0 {
				select {
				case w.updates <- struct{}{}:
				default:
				}
			}
			offset = end
		}
	}
}

func (w *frameWatch) close() error {
	err := w.file.Close()
	<-w.done
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}
