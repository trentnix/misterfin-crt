package playback

import "misterfin-crt/internal/media"

// trackPreparation holds choices and decoder capabilities until refreshed
// source metadata can validate them. It is private to a single Run call.
type trackPreparation struct {
	explicit        *TrackOptions
	saved           *videoPreference
	clientSubtitles bool
	livePicture     bool
}

func prepareTrackChoices(c accountIdentity, config Config, request Request) trackPreparation {
	t := trackPreparation{explicit: request.Tracks}
	if config.Preferences != nil && t.explicit == nil && request.Item.Type != "Audio" && !media.IsLive(request.Item) {
		t.saved = config.Preferences.load(preferenceKey(c, request.Item.ID))
	}
	return t
}

// picture resolves the initial decoder fit before metadata validation. It does
// not replace explicit track choices or expose saved state through Request.
func (t trackPreparation) picture() PictureMode {
	if t.saved != nil {
		return t.saved.Picture
	}
	if t.explicit != nil {
		return t.explicit.Picture
	}
	return PictureOriginal
}
