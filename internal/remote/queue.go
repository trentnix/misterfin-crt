package remote

import (
	"math/rand/v2"
	"slices"
	"strconv"
)

// Queue owns ordering and repeat behavior, independent of media lookup or decoding.
// Call its methods serially. The zero value is an empty queue with repeat disabled.
type Queue struct {
	entries, original []Entry
	index             int
	serial            uint64
	repeat            RepeatMode
	shuffled          bool
}

// Replace installs IDs in their supplied order and selects index.
func (q *Queue) Replace(ids []string, index int) {
	q.entries = q.makeEntries(ids)
	q.original = nil
	q.shuffled = false
	q.index = max(0, min(index, len(q.entries)-1))
}

func (q *Queue) makeEntries(ids []string) []Entry {
	entries := make([]Entry, len(ids))
	for i, id := range ids {
		q.serial++
		entries[i] = Entry{ID: id, Key: strconv.FormatUint(q.serial, 10)}
	}
	return entries
}

// Append inserts after the current entry when next is true, or at the end.
func (q *Queue) Append(ids []string, next bool) {
	entries := q.makeEntries(ids)
	at := len(q.entries)
	if next && len(q.entries) > 0 {
		at = q.index + 1
	}
	q.entries = slices.Insert(q.entries, at, entries...)
	if q.shuffled {
		originalAt := len(q.original)
		if next {
			originalAt = slices.IndexFunc(q.original, func(e Entry) bool { return e.Key == q.Current().Key }) + 1
		}
		q.original = slices.Insert(q.original, originalAt, entries...)
	}
}

// Current returns the selected occurrence, or an empty entry for an empty queue.
func (q *Queue) Current() Entry {
	if len(q.entries) == 0 {
		return Entry{}
	}
	return q.entries[q.index]
}

// Move selects an adjacent occurrence. Natural completion honors RepeatOne.
// At a boundary it returns false unless RepeatAll permits wrapping.
func (q *Queue) Move(direction int, natural bool) bool {
	if len(q.entries) == 0 || (direction != -1 && direction != 1) {
		return false
	}
	if natural && q.repeat == RepeatOne {
		return true
	}
	index := q.index + direction
	if index < 0 || index >= len(q.entries) {
		if q.repeat != RepeatAll {
			return false
		}
		index = (index + len(q.entries)) % len(q.entries)
	}
	q.index = index
	return true
}

// SetRepeat sets a recognized repeat mode. Invalid modes leave the queue unchanged.
func (q *Queue) SetRepeat(mode RepeatMode) {
	if mode == RepeatNone || mode == RepeatAll || mode == RepeatOne {
		q.repeat = mode
	}
}

// SetShuffle randomizes upcoming entries without interrupting the current item.
// Disabling shuffle restores insertion order while preserving the occurrence.
func (q *Queue) SetShuffle(enabled bool) {
	if q.shuffled == enabled {
		return
	}
	current := q.Current()
	if enabled {
		q.original = slices.Clone(q.entries)
		tail := q.entries[min(q.index+1, len(q.entries)):]
		rand.Shuffle(len(tail), func(i, j int) { tail[i], tail[j] = tail[j], tail[i] })
	} else {
		q.entries = q.original
		q.original = nil
		q.index = slices.IndexFunc(q.entries, func(e Entry) bool { return e.Key == current.Key })
		q.index = max(0, q.index)
	}
	q.shuffled = enabled
}

// Snapshot returns an owned copy for a control source or test.
func (q *Queue) Snapshot() QueueState {
	mode := q.repeat
	if mode == "" {
		mode = RepeatNone
	}
	return QueueState{Entries: slices.Clone(q.entries), Current: q.Current().Key, Repeat: mode, Shuffled: q.shuffled}
}

// Shuffled reports ordering mode without copying the queue for presentation.
func (q *Queue) Shuffled() bool { return q.shuffled }

// Len reports the number of occurrences without allocating a snapshot.
func (q *Queue) Len() int { return len(q.entries) }
