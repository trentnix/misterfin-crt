package remote

import (
	"misterfin-crt/internal/remote"
	"testing"
)

func TestCommandTranslation(t *testing.T) {
	cases := []struct {
		body string
		kind remote.Kind
	}{
		{`{"MessageType":"Play","Data":{"PlayCommand":"PlayNow","ItemIds":["a","b"],"StartIndex":1,"StartPositionTicks":50}}`, remote.Play},
		{`{"MessageType":"Play","Data":{"PlayCommand":"PlayNext","ItemIds":["a"]}}`, remote.Play},
		{`{"MessageType":"Play","Data":{"PlayCommand":"PlayLast","ItemIds":["a"]}}`, remote.Play},
		{`{"MessageType":"Play","Data":{"PlayCommand":"PlayShuffle","ItemIds":["a"]}}`, remote.Play},
		{`{"MessageType":"Playstate","Data":{"Command":"PreviousTrack"}}`, remote.Previous},
		{`{"MessageType":"Playstate","Data":{"Command":"Pause"}}`, remote.Pause},
		{`{"MessageType":"Playstate","Data":{"Command":"Unpause"}}`, remote.Resume},
		{`{"MessageType":"Playstate","Data":{"Command":"Seek","SeekPositionTicks":0}}`, remote.Seek},
		{`{"MessageType":"GeneralCommand","Data":{"Name":"SetRepeatMode","Arguments":{"RepeatMode":"RepeatAll"}}}`, remote.Repeat},
		{`{"MessageType":"GeneralCommand","Data":{"Name":"SetShuffleQueue","Arguments":{"ShuffleMode":"Shuffle"}}}`, remote.Shuffle},
		{`{"MessageType":"GeneralCommand","Data":{"Name":"DisplayMessage","Arguments":{"Text":"hello\nworld"}}}`, remote.Message},
	}
	for _, c := range cases {
		cmd, ok := decode([]byte(c.body))
		if !ok || cmd.Kind != c.kind {
			t.Fatalf("decode %s: %+v %v", c.body, cmd, ok)
		}
	}
	for _, s := range []string{`{}`, `null`, `{"MessageType":"Playstate","Data":{"Command":"Seek"}}`, `{"MessageType":"Playstate","Data":{"Command":"Seek","SeekPositionTicks":-1}}`, `{"MessageType":"Play","Data":{"PlayCommand":"PlayNow","ItemIds":["a"],"StartIndex":2}}`, `{"MessageType":"GeneralCommand","Data":{"Name":"SetVolume","Arguments":{"Volume":"0"}}}`} {
		if _, ok := decode([]byte(s)); ok {
			t.Fatal("accepted invalid or unsupported command", s)
		}
	}
}

func TestHeartbeatInterval(t *testing.T) {
	if keepAliveInterval([]byte(`{"MessageType":"ForceKeepAlive","Data":10}`)) != 5 {
		t.Fatal("ignored interval")
	}
	if keepAliveInterval([]byte(`{"MessageType":"ForceKeepAlive","Data":"60"}`)) != 20 {
		t.Fatal("ignored string interval")
	}
}
