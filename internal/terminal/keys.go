package terminal

import "time"

type Decoder struct {
	pending []byte
	last    time.Time
}

func (d *Decoder) Feed(b []byte, now time.Time) []string {
	if len(b) > 0 {
		d.pending = append(d.pending, b...)
		d.last = now
	}
	var keys []string
	for len(d.pending) > 0 {
		c := d.pending[0]
		if c == 27 {
			if len(d.pending) == 1 {
				if now.Sub(d.last) < 50*time.Millisecond {
					break
				}
				keys = append(keys, "back")
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
				key := map[string]string{"A": "up", "B": "down", "C": "next", "D": "previous", "5~": "previous", "6~": "next"}[seq]
				if key != "" {
					keys = append(keys, key)
				}
				d.pending = d.pending[end+1:]
				continue
			}
			keys = append(keys, "back")
			d.pending = d.pending[1:]
			continue
		}
		d.pending = d.pending[1:]
		key := map[byte]string{9: "select", 'q': "quit", 'Q': "quit", 'a': "back", 'A': "back", 'z': "back", 'Z': "back", 127: "back", 8: "back", 'b': "open", 'B': "open", 'x': "open", 'X': "open", 13: "open", 10: "open", 'r': "retry", 'R': "retry", 'j': "down", 'k': "up"}[c]
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}
