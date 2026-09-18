package browser

import (
	"context"
	"sync"

	"mistervision/internal/playback"
	"mistervision/internal/remote"
	"mistervision/internal/rendering"
)

// remoteSession owns the control source for one authenticated account. Commands
// enter the same event loop as physical input without changing button labels.
type remoteSession struct {
	source     remote.Source
	cancel     context.CancelFunc
	workers    sync.WaitGroup
	generation int
}

func (s *browserSession) startRemote() {
	s.stopRemote()
	source := s.controlSource
	if source == nil || s.about.Updating || !s.update.exitAt.IsZero() {
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.remote.source, s.remote.cancel = source, cancel
	generation := s.remote.generation
	s.remote.workers.Add(1)
	go func() {
		defer s.remote.workers.Done()
		source.Run(ctx, func(command remote.Command) { s.send(ctx, remoteCommandResult{generation, command}) })
	}()
}

func (s *browserSession) stopRemote() {
	if s.remote.cancel != nil {
		s.remote.cancel()
	}
	s.remote.generation++
	s.remote.source = nil
	s.remoteRequests.cancelAll()
	s.playbackQueue.active = false
	s.playbackQueue.switching = false
	s.playbackQueue.queue.Replace(nil, 0)
	s.playbackQueue.items = nil
	s.playbackQueue.localRows = nil
}

type remoteCommandResult struct {
	generation int
	command    remote.Command
}

func (r remoteCommandResult) apply(s *browserSession) bool {
	if r.generation != s.remote.generation || s.setup.Kind != rendering.SetupHidden || s.connection.forgetting {
		return false
	}
	return s.handleRemote(r.command)
}

func (s *browserSession) publishRemoteQueue() {
	if s.remote.source == nil {
		return
	}
	state := s.playbackQueue.queue.Snapshot()
	s.remote.source.Publish(state)
	if s.controller.running {
		s.controller.sendCommand(playback.Report)
	}
}
