package playback

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"misterfin-go/internal/jellyfin"
)

// progressReport is an owned snapshot. Initial start/progress and final stop/save
// are indivisible jobs, so another update cannot overtake either lifecycle edge.
type progressReport struct {
	event             string
	state             jellyfin.PlayState
	save, played      bool
	failed, completed bool
}

// progressReporter serializes Jellyfin writes outside the decoder loop. It keeps
// one initial report, one replaceable progress report, and one final report.
// Only the worker performs HTTP requests. The mutex protects short mailbox edits,
// never network I/O. A paused/resumed state supersedes older pending progress.
type progressReporter struct {
	client                  *jellyfin.Client
	live                    bool
	ctx                     context.Context
	cancel                  context.CancelFunc
	mu                      sync.Mutex
	initial, pending, final *progressReport
	wake                    chan struct{}
	done                    chan struct{}
	failed                  atomic.Bool
}

func newProgressReporter(ctx context.Context, client *jellyfin.Client, live bool) *progressReporter {
	ctx, cancel := context.WithCancel(ctx)
	r := &progressReporter{client: client, live: live, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go r.run()
	return r
}

// reportState copies the optional flags as well as the scalar fields. Callers can
// keep updating their session without changing a queued or in-flight request.
func reportState(state jellyfin.PlayState) jellyfin.PlayState {
	if state.CanSeek != nil {
		value := *state.CanSeek
		state.CanSeek = &value
	}
	if state.Failed != nil {
		value := *state.Failed
		state.Failed = &value
	}
	return state
}

// start is called once, after the decoder's first position. The initial progress
// report stays paired with start even if subsequent progress is coalesced.
func (r *progressReporter) start(state jellyfin.PlayState) {
	r.mu.Lock()
	if r.final == nil {
		r.initial = &progressReport{event: "start", state: reportState(state)}
	}
	r.mu.Unlock()
	r.notify()
}

// progress never waits for HTTP. Coalescing retains a pending request to save
// resume data, using the newest position and watched state when it is delivered.
func (r *progressReporter) progress(state jellyfin.PlayState, save, played bool) {
	r.mu.Lock()
	if r.final == nil {
		if r.pending != nil {
			save = save || r.pending.save
		}
		r.pending = &progressReport{event: "progress", state: reportState(state), save: save && !r.live, played: played}
	}
	r.mu.Unlock()
	r.notify()
}

// finish closes the mailbox once decoder and source cleanup are complete. It
// replaces pending progress with the final state. Normal completion lets the
// initial or in-flight report finish. Cancellation, decoder failure, and async
// handoffs abort routine HTTP work. Stop/save has its own five-second context.
// The result reports routine reporting failures, matching Run's existing policy.
func (r *progressReporter) finish(state jellyfin.PlayState, started, played, failed bool, async <-chan struct{}) bool {
	asynchronous := false
	select {
	case <-async:
		asynchronous = true
	default:
	}
	r.mu.Lock()
	r.final = &progressReport{event: "stopped", state: reportState(state), save: started && !r.live, played: played, failed: failed, completed: !failed && r.ctx.Err() == nil}
	r.pending = nil
	if !r.final.completed || asynchronous {
		r.cancel()
	}
	r.mu.Unlock()
	r.notify()
	if !asynchronous {
		select {
		case <-r.done:
		case <-async:
			// Stop can arrive after natural completion began final reporting.
			r.cancel()
		}
	}
	return r.failed.Load()
}

func (r *progressReporter) notify() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// take prioritizes start, then finalization, then replaceable progress. A final
// report remains in the mailbox to prevent later submissions from reopening it.
func (r *progressReporter) take() *progressReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.initial != nil {
		job := r.initial
		r.initial = nil
		return job
	}
	if r.final != nil {
		return r.final
	}
	job := r.pending
	r.pending = nil
	return job
}

func (r *progressReporter) run() {
	defer close(r.done)
	defer r.cancel()
	for {
		job := r.take()
		if job == nil {
			<-r.wake
			continue
		}
		ctx := r.ctx
		if job.event == "stopped" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if r.live {
				failed := job.failed || (job.completed && r.failed.Load())
				job.state.Failed = &failed
			}
			// Final stop/save errors remain best effort, as before the extraction.
			_ = r.client.ReportPlaying(ctx, "stopped", job.state)
			if job.save {
				_ = r.client.SavePlaybackPosition(ctx, job.state.ItemID, job.state.PositionTicks, job.played)
			}
			return
		}
		if job.event == "start" {
			r.record(ctx, r.client.ReportPlaying(ctx, "start", job.state))
		}
		r.record(ctx, r.client.ReportPlaying(ctx, "progress", job.state))
		if job.save {
			r.record(ctx, r.client.SavePlaybackPosition(ctx, job.state.ItemID, job.state.PositionTicks, job.played))
		}
	}
}

// Cancellation is intentional at a decoder handoff. Other routine failures are
// remembered without interrupting controls or otherwise successful decoding.
func (r *progressReporter) record(ctx context.Context, err error) {
	if err != nil && !errors.Is(ctx.Err(), context.Canceled) {
		r.failed.Store(true)
	}
}
