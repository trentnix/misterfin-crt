package browser

import (
	"context"
	"image"
	"sync"
	"time"

	"mistervision/internal/connection"
)

// profileArtworkWork loads each profile once per connection attempt. Only the
// connection worker calls start. Its completion joins these bounded jobs after
// delivering authentication, so artwork cannot delay selection or browsing.
type profileArtworkWork struct {
	ctx        context.Context
	generation int
	send       func(context.Context, workerResult)
	requested  map[string]bool
	jobs       sync.WaitGroup
}

// start schedules missing avatars without waiting for network or image decoding.
// Each batch has a shared deadline and at most four concurrent requests.
func (w *profileArtworkWork) start(profiles []connection.Profile, source connection.ProfileAvatars) {
	if source == nil {
		return
	}
	var ids []string
	for _, profile := range profiles {
		if profile.Avatar == nil && !w.requested[profile.ID] {
			ids = append(ids, profile.ID)
			w.requested[profile.ID] = true
		}
	}
	if len(ids) == 0 {
		return
	}
	w.jobs.Add(1)
	go func() {
		defer w.jobs.Done()
		ctx, cancel := context.WithTimeout(w.ctx, 3*time.Second)
		defer cancel()
		var jobs sync.WaitGroup
		limit := make(chan struct{}, 4)
		for _, id := range ids {
			jobs.Add(1)
			go func(id string) {
				defer jobs.Done()
				select {
				case limit <- struct{}{}:
					defer func() { <-limit }()
				case <-ctx.Done():
					return
				}
				avatar, err := source.Load(ctx, id)
				if err == nil && avatar != nil {
					w.send(ctx, profileAvatarResult{generation: w.generation, id: id, avatar: avatar})
				}
			}(id)
		}
		jobs.Wait()
	}()
}

// wait joins artwork workers before the attempt's completion channel closes.
func (w *profileArtworkWork) wait() { w.jobs.Wait() }

// profileAvatarResult updates public artwork without resetting selection, PIN
// input, or status. Generation checks discard images from canceled connections.
type profileAvatarResult struct {
	generation int
	id         string
	avatar     image.Image
}

func (r profileAvatarResult) apply(s *browserSession) bool {
	if !s.connection.current(r.generation) {
		return false
	}
	if s.connection.profileAvatars == nil {
		s.connection.profileAvatars = make(map[string]image.Image)
	}
	s.connection.profileAvatars[r.id] = r.avatar
	for i, profile := range s.setup.Profiles {
		if profile.ID == r.id {
			s.setup.Profiles = append([]connection.Profile(nil), s.setup.Profiles...)
			s.setup.Profiles[i].Avatar = r.avatar
			break
		}
	}
	if s.about.Profile != nil && s.about.Profile.ID == r.id {
		profile := *s.about.Profile
		profile.Avatar = r.avatar
		s.about.Profile = &profile
	}
	return true
}
