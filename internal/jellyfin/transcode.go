package jellyfin

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// TranscodeProfile limits server-side video conversion. Dimensions are maximums,
// not an output aspect ratio. Zero fields use the existing defaults. Config files
// must specify widths of 160–1920, heights of 120–1080, and bitrates of
// 100,000–50,000,000 bits per second. Programmatic callers must use the same ranges.
// The display pipeline, not this profile, selects the frame-rate cap.
type TranscodeProfile struct {
	MaxWidth, MaxHeight int
	VideoBitrate        int
}

// DefaultTranscodeProfile preserves the established recorded-video and Live TV
// limits. Live TV uses VideoBitrate as its negotiated streaming bitrate budget.
func DefaultTranscodeProfile() TranscodeProfile {
	return TranscodeProfile{MaxWidth: 720, MaxHeight: 576, VideoBitrate: 12000000}
}

// transcodeProfile fills omitted fields for callers that construct Config
// directly. Parsed profiles are already complete and validated.
func (c Config) transcodeProfile() TranscodeProfile {
	p, defaults := c.Transcode, DefaultTranscodeProfile()
	if p.MaxWidth == 0 {
		p.MaxWidth = defaults.MaxWidth
	}
	if p.MaxHeight == 0 {
		p.MaxHeight = defaults.MaxHeight
	}
	if p.VideoBitrate == 0 {
		p.VideoBitrate = defaults.VideoBitrate
	}
	return p
}

// Reserve profile-shaped lines even when malformed so an invalid profile cannot
// silently become an API key. This prefix is reserved for profile settings.
var profileLine = regexp.MustCompile(`^[+-]?\d+\s*[xX]`)

// parseTranscodeProfile follows the C WIDTHxHEIGHT[@BITRATE] format. Omitting
// bitrate retains the preceding value. Invalid input returns no partial update.
// Errors never include the line, which belongs to a credential-bearing file.
func parseTranscodeProfile(line string, current TranscodeProfile) (TranscodeProfile, error) {
	dimensions, bitrate, hasBitrate := strings.Cut(line, "@")
	width, height, ok := strings.Cut(dimensions, "x")
	decimal := func(s string) (int, bool) {
		if s == "" {
			return 0, false
		}
		for _, c := range s {
			if c < '0' || c > '9' {
				return 0, false
			}
		}
		n, err := strconv.Atoi(s)
		return n, err == nil
	}
	w, validWidth := decimal(width)
	h, validHeight := decimal(height)
	rate, validRate := current.VideoBitrate, true
	if hasBitrate {
		rate, validRate = decimal(bitrate)
	}
	if !ok || !validWidth || !validHeight || !validRate {
		return current, errors.New("use WIDTHxHEIGHT or WIDTHxHEIGHT@BITRATE")
	}
	if w < 160 || w > 1920 || h < 120 || h > 1080 {
		return current, errors.New("width must be 160-1920 and height must be 120-1080")
	}
	if rate < 100000 || rate > 50000000 {
		return current, errors.New("bitrate must be 100000-50000000 bits per second")
	}
	return TranscodeProfile{MaxWidth: w, MaxHeight: h, VideoBitrate: rate}, nil
}
