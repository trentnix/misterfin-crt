package remote

import (
	"reflect"
	"testing"
)

func TestQueueOccurrencesAndBoundaries(t *testing.T) {
	var q Queue
	q.Replace([]string{"a", "b", "a"}, 1)
	q.Append([]string{"next"}, true)
	q.Append([]string{"last"}, false)
	state := q.Snapshot()
	ids := []string{}
	keys := map[string]bool{}
	for _, e := range state.Entries {
		ids = append(ids, e.ID)
		if keys[e.Key] {
			t.Fatal("duplicate occurrence")
		}
		keys[e.Key] = true
	}
	if !reflect.DeepEqual(ids, []string{"a", "b", "next", "a", "last"}) {
		t.Fatal(ids)
	}
	if !q.Move(-1, false) || q.Current().ID != "a" || q.Move(-1, false) {
		t.Fatal("previous boundary")
	}
	q.SetRepeat(RepeatAll)
	if !q.Move(-1, false) || q.Current().ID != "last" {
		t.Fatal("wrap")
	}
	q.SetRepeat(RepeatOne)
	before := q.Current()
	if !q.Move(1, true) || q.Current() != before {
		t.Fatal("repeat one")
	}
	if !q.Move(-1, false) || q.Current() == before {
		t.Fatal("manual previous repeated current track")
	}
}

func TestShufflePreservesCurrentAndRestoresOrder(t *testing.T) {
	var q Queue
	q.Replace([]string{"a", "b", "c", "d", "e", "f"}, 1)
	original := q.Snapshot()
	current := q.Current()
	q.SetShuffle(true)
	if q.Current() != current || !q.Snapshot().Shuffled {
		t.Fatal("shuffle moved current item")
	}
	q.Move(1, false)
	selected := q.Current()
	q.SetShuffle(false)
	restored := q.Snapshot()
	if q.Current() != selected || !reflect.DeepEqual(restored.Entries, original.Entries) {
		t.Fatal("unshuffle lost order or selection")
	}
	restored.Entries[0].ID = "changed"
	if q.Snapshot().Entries[0].ID != "a" {
		t.Fatal("snapshot aliases queue")
	}
}

func TestUnshuffleKeepsPlayNextInsertion(t *testing.T) {
	var q Queue
	q.Replace([]string{"a", "b", "c", "d"}, 1)
	q.SetShuffle(true)
	q.Append([]string{"next"}, true)
	q.SetShuffle(false)
	got := q.Snapshot().Entries
	if len(got) != 5 || got[2].ID != "next" || q.Current().ID != "b" {
		t.Fatal(got)
	}
}
