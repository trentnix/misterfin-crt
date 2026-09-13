package browser

import (
	"context"

	"misterfin-go/internal/jellyfin"
	"misterfin-go/internal/playback"
)

// playbackProcess holds one decoder's lifecycle resources. Only the controller
// closes its gate and cleanup signal. Completion comes back as an event.
type playbackProcess struct {
	id      int
	cancel  context.CancelFunc
	done    chan struct{} // Bridge closes when playback.Run returns.
	cleanup chan struct{} // Closing allows server cleanup to run asynchronously.
	gate    chan struct{} // Closing permits a prepared replacement to start decoding.
	ready   bool          // Preparation succeeded. The decoder may still be waiting on its gate.
}

func (p *playbackProcess) stop() {
	if p.cancel != nil {
		p.cancel()
	}
}

// stopWithAsyncCleanup cancels decoding without waiting for Jellyfin stop/save
// or tuner release before the handoff. The cleanup signal closes at most once.
func (p *playbackProcess) stopWithAsyncCleanup() {
	if p.cleanup != nil {
		close(p.cleanup)
		p.cleanup = nil
	}
	p.stop()
}

// playbackLaunch starts asynchronous work and returns its lifecycle handles.
// If prepare is true, the bridge reports PlaybackPrepared before waiting on gate.
// The controller owns gate closure. The bridge owns done closure and event IDs.
type playbackLaunch func(
	item jellyfin.Item,
	offset *int64,
	gate <-chan struct{},
	prepare bool,
	controls chan playback.Control,
) playbackProcess

// allowStart transfers a prepared decoder from waiting to running. The controller
// calls it once, after the previous decoder has reported completion.
func (p *playbackProcess) allowStart() {
	close(p.gate)
	p.gate = nil
}

func (p *playbackProcess) wait() {
	if p.done != nil {
		<-p.done
	}
}
