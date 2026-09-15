package browser

import (
	"context"
	"errors"
	"time"

	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/playback"
	"misterfin-crt/internal/remote"
)

const (
	remoteRequestTimeout     = 30 * time.Second
	maxPendingRemoteRequests = 32
)

// remoteRequests owns catalog work independently of the active playback queue.
// Remote additions resolve serially to preserve their order. Local album lookup
// has a separate generation so canceled results cannot replace a newer queue.
// Only the browser event loop changes these fields.
type remoteRequests struct {
	localCancel     context.CancelFunc
	localGeneration int
	cancel          context.CancelFunc
	generation      int
	resolving       bool
	requests        []remote.Command
}

type remoteItemsResult struct {
	generation int
	command    remote.Command
	items      []jellyfin.Item
	err        error
}

func (r remoteItemsResult) apply(s *browserSession) bool {
	q := &s.remoteRequests
	if r.generation != q.generation {
		return false
	}
	q.resolving = false
	q.cancel = nil
	if r.err != nil {
		s.message = MessagePresentation{Header: "Remote playback", Text: "Could not load the requested queue.", Until: time.Now().Add(8 * time.Second)}
	} else {
		s.applyRemoteItems(r.command, r.items)
	}
	if len(q.requests) > 0 {
		cmd := q.requests[0]
		q.requests[0] = remote.Command{}
		q.requests = q.requests[1:]
		s.resolveRemotePlay(cmd)
	}
	return true
}

// cancelAll invalidates catalog results without stopping the current decoder.
func (q *remoteRequests) cancelAll() {
	if q.cancel != nil {
		q.cancel()
	}
	q.cancel = nil
	q.cancelLocal()
	q.generation++
	q.resolving = false
	q.requests = nil
}

func (s *browserSession) requestRemotePlay(cmd remote.Command) {
	q := &s.remoteRequests
	if cmd.PlayMode == remote.PlayNow || cmd.PlayMode == remote.PlayShuffle || cmd.PlayMode == remote.PlayMix {
		s.remoteRequests.cancelAll()
	}
	if q.resolving {
		if len(q.requests) < maxPendingRemoteRequests {
			q.requests = append(q.requests, cmd)
		}
		return
	}
	s.resolveRemotePlay(cmd)
}

func (s *browserSession) resolveRemotePlay(cmd remote.Command) {
	q := &s.remoteRequests
	ctx, cancel := context.WithTimeout(s.ctx, remoteRequestTimeout)
	q.cancel = cancel
	q.resolving = true
	generation, client := q.generation, s.client
	go func() {
		defer cancel()
		items, err := client.RemoteItems(ctx, cmd.IDs, cmd.PlayMode == remote.PlayMix)
		if err == nil {
			for _, item := range items {
				if !playback.Supported(item) {
					err = errors.New("unsupported remote media")
					break
				}
			}
			if len(items) == 0 {
				err = errors.New("empty remote queue")
			}
		}
		s.send(s.ctx, remoteItemsResult{generation, cmd, items, err})
	}()
}

// cancelLocal invalidates a background album lookup before playback changes.
func (q *remoteRequests) cancelLocal() {
	if q.localCancel != nil {
		q.localCancel()
	}
	q.localCancel = nil
	q.localGeneration++
}
