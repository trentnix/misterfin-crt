package feedback

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"mistervision/internal/player"
)

// ParseANS accepts the shared patched-MPlayer/Python status protocol. It returns
// only validated positions, amplitudes, cache state, captions, and acknowledgments.
func ParseANS(line string) (player.Feedback, bool) {
	if text, ok := parseCaption(line); ok {
		return player.Feedback{Kind: player.FeedbackCaption, Caption: text}, true
	}
	if strings.HasPrefix(line, "ANS_PICTURE_MODE=") {
		var request, mode int
		if n, err := fmt.Sscanf(line, "ANS_PICTURE_MODE=%d,%d", &request, &mode); n == 2 && err == nil && request > 0 && mode >= -1 && mode <= 1 {
			result := player.PictureResult{Request: request, Mode: player.PictureMode(max(0, mode))}
			if mode < 0 {
				result.Err = errors.New("cannot change picture mode")
			}
			return player.Feedback{Kind: player.FeedbackPicture, Picture: result}, true
		}
		return player.Feedback{}, false
	}
	if line == "ANS_VIDEO_STARTED=true" {
		return player.Feedback{Kind: player.FeedbackVideoStarted}, true
	}
	if line == "ANS_BUFFERING=true" || line == "ANS_BUFFERING=false" {
		return player.Feedback{Kind: player.FeedbackBuffering, Buffering: line == "ANS_BUFFERING=true"}, true
	}
	if levels, ok := parseAudioLevels(line); ok {
		return player.Feedback{Kind: player.FeedbackLevels, Levels: levels}, true
	}
	if value, ok := strings.CutPrefix(line, "ANS_TIME_POSITION="); ok {
		if seconds, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) && seconds >= 0 && seconds < 1e9 {
			return player.Feedback{Kind: player.FeedbackPosition, Position: seconds}, true
		}
	}
	return player.Feedback{}, false
}
