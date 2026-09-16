package media

import (
	"testing"
)

func TestPlaySessionIDs(t *testing.T) {
	first, err := NewPlaySessionID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewPlaySessionID()
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first == second {
		t.Fatal("play session IDs must be nonempty and unique")
	}
}
