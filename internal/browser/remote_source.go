package browser

import (
	"context"
	"sync"

	"misterfin-crt/internal/remote"
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
	if s.config.Remote == nil {
		return
	}
	source := s.config.Remote(s.client)
	if source == nil {
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
	s.remotePlayback.active = false
	s.remotePlayback.switching = false
	s.remotePlayback.queue.Replace(nil, 0)
	s.remotePlayback.items = nil
}

type remoteCommandResult struct {
	generation int
	command    remote.Command
}

func (r remoteCommandResult) apply(s *browserSession) bool {
	if r.generation != s.remote.generation || s.setup.Kind != SetupHidden {
		return false
	}
	return s.handleRemote(r.command)
}

func (s *browserSession) publishRemoteQueue() {
	if s.remote.source == nil {
		return
	}
	state := s.remotePlayback.queue.Snapshot()
	s.remote.source.Publish(state)
	if s.controller.running {
		s.controller.sendCommand("report")
	}
}
