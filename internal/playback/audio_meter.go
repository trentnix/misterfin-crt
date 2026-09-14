package playback

import "os"

// levelConfigurer prepares a decoder's optional audio feedback transport.
// The returned decoder keeps immutable launch settings. A non-nil meter is
// owned by Run until the process exits. Nil means feedback arrives through the
// decoder's status pipe, or metering could not be enabled.
type levelConfigurer interface {
	withAudioLevels() (decoder, *audioMeter)
}

func configureAudioLevels(d decoder, enabled bool) (decoder, *audioMeter) {
	if configurable, ok := d.(levelConfigurer); enabled && ok {
		return configurable.withAudioLevels()
	}
	return d, nil
}

// audioMeter owns one MPlayer export file. The playback loop samples it while
// running. Run removes it only after reaping the process that writes it.
type audioMeter struct{ path string }

func newAudioMeter() *audioMeter {
	file, err := os.CreateTemp("", "misterfin-crt-audio-*")
	if err != nil {
		return nil // Missing meters must not prevent music playback.
	}
	file.Close()
	return &audioMeter{path: file.Name()}
}

func (m *audioMeter) levels() AudioLevels { return audioExport(m.path) }

func (m *audioMeter) close() {
	if m != nil {
		_ = os.Remove(m.path)
	}
}
