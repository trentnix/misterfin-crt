package control

import (
	"encoding/json"
	"testing"
)

func TestBindingNamesAndJSONCompatibility(t *testing.T) {
	// Literal spellings are the public configuration contract.
	for _, name := range []string{"", "about", "up", "down", "previous", "next", "open", "back", "select", "retry", "quit", "track-previous", "track-next", "seek-backward", "seek-forward"} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(name)
			if err != nil {
				t.Fatal(err)
			}
			var action Action
			if err := json.Unmarshal(data, &action); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(action)
			if err != nil || string(encoded) != string(data) {
				t.Fatal("binding spelling changed", string(encoded), err)
			}
		})
	}
	for _, name := range []string{"seek-forwards", "UP", " up", "up-repeat", "controls"} {
		action := Open
		data, _ := json.Marshal(name)
		if err := json.Unmarshal(data, &action); err == nil || action != Open {
			t.Fatal("invalid action accepted or destination changed", name)
		}
	}
	for _, data := range []string{"42", "true", "[]", "{}"} {
		var action Action
		if json.Unmarshal([]byte(data), &action) == nil {
			t.Fatal("non-string action accepted", data)
		}
	}
}

func TestRepeatRetainsActionWithoutBecomingABinding(t *testing.T) {
	for _, action := range []Action{Up, Down, Previous, Next, About, Open, Back, Select, TrackPrevious, TrackNext, SeekBackward, SeekForward} {
		repeated := action.Repeat()
		if !repeated.IsRepeat() || repeated.Base() != action || repeated.Repeat() != repeated || action.IsRepeat() {
			t.Fatal("repeat identity lost", action)
		}
		if _, err := ParseBinding(string(repeated)); err == nil {
			t.Fatal("repeat accepted as a configured binding")
		}
	}
	if Action("").Repeat() != "" {
		t.Fatal("disabled action gained a repeat")
	}
}
