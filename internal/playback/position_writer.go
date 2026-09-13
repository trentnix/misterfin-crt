package playback

import (
	"math"
	"strconv"
	"strings"
	"sync"
)

// positionWriter parses numeric progress, first-frame feedback, and boolean
// cache status. Diagnostics may contain media URLs and must never be copied
// to terminal output or error messages.
type positionWriter struct {
	mu           sync.Mutex
	pending      string
	positions    chan float64
	levels       chan AudioLevels
	buffering    chan bool
	videoStarted chan struct{}
}

// Write accepts concurrent decoder stdout and stderr writes. It retains partial
// lines and drops feedback when the corresponding channel is full, so parsing
// cannot stall decoding. It reports every input byte consumed without logging it.
func (p *positionWriter) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, b := range data {
		if b == '\r' || b == '\n' {
			line := strings.TrimSpace(p.pending)
			p.pending = ""
			if line == "ANS_VIDEO_STARTED=true" {
				select {
				case p.videoStarted <- struct{}{}:
				default:
				}
				continue
			}
			if line == "ANS_BUFFERING=true" || line == "ANS_BUFFERING=false" {
				select {
				case p.buffering <- line == "ANS_BUFFERING=true":
				default:
				}
				continue
			}
			if levels, ok := parseAudioLevels(line); ok {
				select {
				case p.levels <- levels:
				default:
				}
				continue
			}
			value := ""
			if strings.HasPrefix(line, "ANS_TIME_POSITION=") {
				value = strings.TrimPrefix(line, "ANS_TIME_POSITION=")
			} else if fields := strings.Fields(line); len(fields) > 1 && (fields[1] == "A-V:" || fields[1] == "M-V:" || fields[1] == "M-A:") {
				value = fields[0]
			}
			if seconds, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) && seconds >= 0 && seconds < 1e9 {
				select {
				case p.positions <- seconds:
				default:
				}
			}
		} else if len(p.pending) < 8192 {
			p.pending += string(b)
		}
	}
	return len(data), nil
}
