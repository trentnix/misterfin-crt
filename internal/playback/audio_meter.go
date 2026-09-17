package playback

import playerapi "mistervision/internal/player"

// configureAudioLevels enables an optional feedback transport. Run owns a
// returned meter until decoder cleanup. Status-pipe implementations return nil.
func configureAudioLevels(d playerapi.Decoder, enabled bool) (playerapi.Decoder, playerapi.Meter) {
	if configurable, ok := d.(playerapi.LevelConfigurer); enabled && ok {
		return configurable.WithAudioLevels()
	}
	return d, nil
}
