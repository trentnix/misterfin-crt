package terminal

import (
	"time"

	"misterfin-crt/internal/input/control"
)

// Decoder translates terminal byte sequences into semantic input actions.
// One input reader owns its pending escape sequence and timing state.
type Decoder struct {
	pending []byte
	last    time.Time
}

// Feed accepts another fragment and returns completed actions. Empty input
// resolves a pending Escape after 50 milliseconds. Release events emit no action.
func (d *Decoder) Feed(b []byte, now time.Time) []control.Action {
	if len(b) > 0 {
		d.pending = append(d.pending, b...)
		d.last = now
	}
	var keys []control.Action
	for len(d.pending) > 0 {
		c := d.pending[0]
		if c == 27 {
			if len(d.pending) == 1 {
				if now.Sub(d.last) < 50*time.Millisecond {
					break
				}
				keys = append(keys, control.Back)
				d.pending = d.pending[1:]
				continue
			}
			if d.pending[1] == '[' || d.pending[1] == 'O' {
				end := 2
				for end < len(d.pending) && (d.pending[end] < 0x40 || d.pending[end] > 0x7e) {
					end++
				}
				if end == len(d.pending) {
					if now.Sub(d.last) < 50*time.Millisecond && len(d.pending) < 32 {
						break
					}
					d.pending = nil
					break
				}
				seq := string(d.pending[2 : end+1])
				key := sequenceAction(seq)
				if key != "" {
					keys = append(keys, key)
				}
				d.pending = d.pending[end+1:]
				continue
			}
			keys = append(keys, control.Back)
			d.pending = d.pending[1:]
			continue
		}
		d.pending = d.pending[1:]
		key := characterAction(rune(c))
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}
