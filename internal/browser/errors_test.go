package browser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"mistervision/internal/jellyfin"
	"mistervision/internal/media"
	"mistervision/internal/playback"
	"mistervision/internal/plex"
)

func TestRequestFailuresUseProviderIndependentGuidance(t *testing.T) {
	for _, provider := range []struct {
		name   string
		status func(int) error
	}{
		{"Jellyfin", func(status int) error { return &jellyfin.HTTPError{Status: status} }},
		{"Plex", func(status int) error { return &plex.HTTPError{Status: status} }},
	} {
		for _, tc := range []struct {
			code     int
			want     string
			category error
		}{
			{401, "Sign in again", media.ErrUnauthorized}, {403, "Sign in again", media.ErrUnauthorized},
			{404, "moved or removed", media.ErrNotFound}, {410, "moved or removed", media.ErrNotFound},
			{500, "server reported a problem", media.ErrServerFailure}, {503, "server reported a problem", media.ErrServerFailure},
		} {
			t.Run(fmt.Sprintf("%s/%d", provider.name, tc.code), func(t *testing.T) {
				err := fmt.Errorf("private request: %w", provider.status(tc.code))
				if !errors.Is(err, tc.category) {
					t.Fatal("provider lost shared category")
				}
				m := New()
				r := m.Load(0)
				m.Apply(*r, media.Page{}, err)
				message := m.Current().Error
				if !strings.Contains(message, tc.want) || strings.Contains(message, "private") || strings.Contains(message, "HTTP") {
					t.Fatal(message)
				}
			})
		}
	}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "too long"}, {media.ErrUnavailable, "network"}, {media.ErrTuning, "tuner"}, {media.ErrConversion, "conversion settings"}, {errors.New("private invalid container"), "Try again"},
	} {
		message := requestFailure("Could not load this list.", tc.err)
		if !strings.Contains(message, tc.want) || strings.Contains(message, "private") {
			t.Fatal(message)
		}
	}
}

func TestPlaybackFailureCategories(t *testing.T) {
	for _, tc := range []struct {
		err          error
		header, body string
	}{
		{playback.ErrNotStarted, "Playback didn't start", "could not start"},
		{playback.ErrInterrupted, "Playback interrupted", "stopped unexpectedly"},
		{playback.ErrProgress, "Progress update failed", "resume position"},
		{playback.ErrPlayerUnavailable, "Playback didn't start", "matching player"},
		{playback.ErrTrackUnavailable, "Playback didn't start", "choose another track"},
	} {
		s := testSession(t)
		s.showPlaybackError(fmt.Errorf("private: %w", tc.err))
		if s.message.Header != tc.header || !strings.Contains(s.message.Text, tc.body) || strings.Contains(s.message.Text, "private") {
			t.Fatal(s.message)
		}
	}
}

func TestNavigationAndSubtitleRecoveryGuidance(t *testing.T) {
	for _, direction := range []int{-1, 1} {
		s := testSession(t)
		s.model.Stack = append(s.model.Stack, View{Detail: &media.Item{Type: "Audio"}})
		s.model.StartMusicQueue()
		s.media.nextTrack = direction
		s.media.pending = true
		s.handleNeighbor(neighborResult{generation: s.media.generation, err: errors.New("private")})
		want := "next"
		if direction < 0 {
			want = "previous"
		}
		if !strings.Contains(s.model.Notice, want+" track. Try again.") {
			t.Fatal(s.model.Notice)
		}
	}
	f := newControllerFixture(t)
	f.c.subtitleLoading = true
	f.c.Handle(PlaybackEvent{Kind: PlaybackSubtitle, ID: f.c.active.id, Subtitle: playback.SubtitleResult{Err: errors.New("private")}}, f.now)
	if f.c.notice != "Could not load these subtitles. Try again or choose another track." {
		t.Fatal(f.c.notice)
	}
}
