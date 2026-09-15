package diagnostics

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Log owns a bounded event queue and one disk worker. A nil Log disables logging.
// Record never waits for disk. Close drains accepted events and joins the worker.
// Calls concurrent with or after Close are safe and are discarded.
type Log struct {
	mu      sync.RWMutex
	closed  bool
	queue   chan slog.Record
	done    chan struct{}
	dropped atomic.Uint64
	failed  atomic.Bool
	err     error // Written by the worker and read only after done closes.
}

// Open returns nil when disabled. Enabled logs start a fresh current file with
// mode 0600. Rotation retains at most one previous file. File errors stop logging
// and are returned by Close. They never propagate through Record to playback.
func Open(c Config) (*Log, error) {
	if !c.Enabled {
		return nil, nil
	}
	if c.Path == "" || c.MaxBytes < 4096 || c.MaxBytes > 64<<20 {
		return nil, errors.New("invalid diagnostics configuration")
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0700); err != nil {
		return nil, err
	}
	// Both files belong to this logger. Start each run without stale rotation data.
	if err := os.Remove(c.Path + ".1"); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	w := &rotatingFile{path: c.Path, limit: c.MaxBytes}
	if err := w.open(); err != nil {
		return nil, err
	}
	l := &Log{queue: make(chan slog.Record, 128), done: make(chan struct{})}
	go l.write(w)
	return l, nil
}

// Record queues a structured event. Names and string attributes must be static
// labels or sanitized metadata. Do not pass errors, URLs, headers, user content,
// or mutable values through slog.Any. Excess events are counted and discarded.
func (l *Log) Record(event string, attrs ...slog.Attr) {
	if l == nil || l.failed.Load() {
		return
	}
	record := slog.NewRecord(time.Now(), slog.LevelInfo, event, 0)
	record.AddAttrs(attrs...)
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return
	}
	select {
	case l.queue <- record:
	default:
		l.dropped.Add(1)
	}
}

// Close is idempotent and waits only at application shutdown. It reports a file
// failure without logging its potentially sensitive operating-system message.
func (l *Log) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	if !l.closed {
		l.closed = true
		close(l.queue)
	}
	l.mu.Unlock()
	<-l.done
	return l.err
}

// write performs JSON encoding and all file writes outside application loops.
func (l *Log) write(w *rotatingFile) {
	defer close(l.done)
	defer func() { l.err = errors.Join(l.err, w.file.Close()) }()
	handler := slog.NewJSONHandler(w, nil)
	ctx := context.Background()
	emit := func(record slog.Record) {
		if l.err == nil {
			l.err = handler.Handle(ctx, record)
			if l.err != nil {
				l.failed.Store(true)
			}
		}
	}
	dropped := func() {
		if n := l.dropped.Swap(0); n > 0 {
			record := slog.NewRecord(time.Now(), slog.LevelWarn, "diagnostics.dropped", 0)
			record.AddAttrs(slog.Uint64("events", n))
			emit(record)
		}
	}
	for record := range l.queue {
		dropped()
		emit(record)
	}
	dropped()
}

// rotatingFile is owned by the worker. Each Write contains one complete JSON
// record. Oversized records fail logging instead of violating the size bound.
type rotatingFile struct {
	path        string
	file        *os.File
	size, limit int64
}

func (w *rotatingFile) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	w.file, w.size = f, 0
	return nil
}

func (w *rotatingFile) Write(p []byte) (int, error) {
	if int64(len(p)) > w.limit {
		return 0, errors.New("diagnostic event exceeds file limit")
	}
	if w.size+int64(len(p)) > w.limit {
		if err := w.file.Close(); err != nil {
			return 0, err
		}
		if err := os.Rename(w.path, w.path+".1"); err != nil {
			return 0, err
		}
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}
