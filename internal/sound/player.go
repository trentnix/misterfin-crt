package sound

import (
	"fmt"
	"sync"
	"time"

	"mistervision/assets/sfx"
)

// Player keeps one pending cue and borrows audio only during browsing bursts.
// A single mutex protects playback state and device access. Play skips busy
// workers, while Suspend waits for device release before returning.
// Close must follow browser and playback shutdown. A nil Player is silent.
type Player struct {
	mu          sync.Mutex
	open        OpenFunc
	stream      Stream
	clips       [2][]int16
	current     []int16
	pending     []int16
	silence     [1024]int16
	last, retry time.Time
	suspended   int
	closed      bool
	wake        chan struct{}
	stop, done  chan struct{}
}

// New prepares clips and starts one worker. Disabled feedback opens no resources
// and returns nil. The caller owns the returned Player and must Close it.
func New(cfg Config, open OpenFunc) (*Player, error) {
	if cfg.Volume < 0 || cfg.Volume > 100 {
		return nil, fmt.Errorf("sound volume must be between 0 and 100")
	}
	if !cfg.Enabled || cfg.Volume == 0 || open == nil {
		return nil, nil
	}
	p := &Player{open: open, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	var err error
	for i, data := range [][]byte{sfx.Navigation, sfx.Confirmation} {
		p.clips[i], err = decodeClip(data, cfg.Volume)
		if err != nil {
			return nil, err
		}
	}
	go p.run()
	return p, nil
}

// Play queues a cue without waiting. Confirmation can replace a queued click.
// Held navigation never builds a backlog of sounds after scrolling stops.
func (p *Player) Play(c Cue) {
	if p == nil || c > Confirm || !p.mu.TryLock() {
		return
	}
	defer p.mu.Unlock()
	if p.closed || p.suspended > 0 {
		return
	}
	if p.pending == nil || c == Confirm {
		p.pending = p.clips[c]
	}
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Suspend drops queued audio and closes the device before a decoder may open it.
// Releases are counted so a seeking replacement cannot unmute an active player.
func (p *Player) Suspend() func() {
	if p == nil {
		return func() {}
	}
	p.mu.Lock()
	p.suspended++
	p.pending = nil
	p.clear()
	p.mu.Unlock()
	return sync.OnceFunc(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.suspended--
	})
}

// Close stops the worker and releases audio. Repeated calls are safe.
func (p *Player) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		p.pending = nil
		p.clear()
		close(p.stop)
	}
	p.mu.Unlock()
	<-p.done
}

// clear drops the current clip and closes the device while mu is held.
func (p *Player) clear() {
	p.current = nil
	if p.stream != nil {
		_ = p.stream.Close()
		p.stream = nil
	}
}

// run sleeps completely when idle. A short timer feeds nonblocking audio only
// during a navigation burst. No processes or files are created per button press.
func (p *Player) run() {
	defer close(p.done)
	timer := time.NewTicker(5 * time.Millisecond)
	defer timer.Stop()
	timer.Stop()
	var tick <-chan time.Time
	for {
		select {
		case <-p.stop:
			return
		case <-p.wake:
			p.mu.Lock()
			if p.pending != nil && time.Now().After(p.retry) {
				p.current = p.pending
				p.last = time.Now()
				timer.Reset(5 * time.Millisecond)
				tick = timer.C
			}
			p.pending = nil
			p.mu.Unlock()
		case now := <-tick:
			p.mu.Lock()
			active := p.write(now)
			p.mu.Unlock()
			if !active {
				timer.Stop()
				tick = nil
			}
		}
	}
}

// write feeds a short PCM block while mu is held. Silence keeps a device ready
// briefly between clicks. Idle, unavailable, and suspended devices are released.
func (p *Player) write(now time.Time) bool {
	if p.closed || p.suspended > 0 || now.Sub(p.last) > 200*time.Millisecond || (p.stream == nil && p.current == nil) {
		p.clear()
		return false
	}
	if p.stream == nil {
		var err error
		p.stream, err = p.open()
		if err != nil || p.stream == nil {
			p.retry = now.Add(time.Second)
			p.clear()
			return false
		}
	}
	samples := p.current
	if len(samples) == 0 {
		samples = p.silence[:]
	}
	samples = samples[:min(len(samples), len(p.silence))]
	n, err := p.stream.Write(samples)
	if err != nil || n < 0 || n > len(samples)/2 {
		p.retry = now.Add(time.Second)
		p.clear()
		return false
	}
	if len(p.current) > 0 {
		p.current = p.current[n*2:]
	}
	return true
}
