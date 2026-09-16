package plex

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"misterfin-crt/internal/media"
)

func TestLiveAudioSelectionUsesFreshIDs(t *testing.T) {
	var tunes, selected, decisions atomic.Int32
	c := liveClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/tune"):
			n := tunes.Add(1)
			fmt.Fprintf(w, `{"MediaContainer":{"Metadata":[{"Media":[{"uuid":"live-source","Part":[{"id":"%d","Stream":[{"streamType":1,"index":0,"id":"1"},{"streamType":2,"index":3,"id":"%d","displayTitle":"English (AC3 Stereo)","selected":true},{"streamType":2,"index":7,"id":"%d","language":"Español","displayTitle":"Español Descriptive (AC3 Stereo)"}]}]}]}]}}`, n*100, n*100+1, n*100+2)
		case strings.HasPrefix(r.URL.Path, "/library/parts/"):
			n := tunes.Load()
			if r.Method != "PUT" || r.URL.Path != fmt.Sprintf("/library/parts/%d", n*100) || r.URL.Query().Get("audioStreamID") != fmt.Sprint(n*100+2) {
				t.Error("selection reused an old Plex ID or a broadcast index")
			}
			selected.Add(1)
		case strings.HasSuffix(r.URL.Path, "/decision"):
			if selected.Load() != tunes.Load() {
				t.Error("conversion started before audio selection")
			}
			decisions.Add(1)
			fmt.Fprint(w, `{"MediaContainer":{"generalDecisionCode":1001}}`)
		case strings.HasSuffix(r.URL.Path, "/stop"), strings.HasPrefix(r.URL.Path, "/media/grabbers/operations/"):
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	})
	for range 2 {
		stream, err := c.PrepareLive(t.Context(), media.LiveRequest{ChannelID: liveID("2", "channel"), MaxFrameRate: 30, AudioIndex: 7})
		if err != nil {
			t.Fatal(err)
		}
		if !stream.LiveAudio || len(stream.Streams) != 3 || stream.Streams[2].Index != 7 || stream.Streams[2].Label() != "Español Descriptive (AC3 Stereo)" {
			t.Fatalf("live track metadata: %+v", stream.Streams)
		}
		if err := stream.Release(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if selected.Load() != 2 || decisions.Load() != 2 {
		t.Fatal("audio was not selected for each replacement")
	}
}

func TestLiveAudioFailuresReleaseConsumer(t *testing.T) {
	for _, test := range []struct {
		name          string
		index, status int
	}{
		{"missing track", 99, 200}, {"selection rejected", 2, 403}, {"server failure", 2, 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			var released, stopped atomic.Int32
			c := liveClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/tune"):
					fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"Media":[{"uuid":"live-source","Part":[{"id":10,"Stream":[{"id":11,"streamType":2,"index":1},{"id":12,"streamType":2,"index":2}]}]}]}]}}`)
				case strings.HasPrefix(r.URL.Path, "/library/parts/"):
					if test.index == 99 {
						t.Error("missing track attempted selection")
					}
					w.WriteHeader(test.status)
				case strings.HasSuffix(r.URL.Path, "/stop"):
					stopped.Add(1)
				case strings.HasPrefix(r.URL.Path, "/media/grabbers/operations/"):
					if r.Method != "DELETE" {
						t.Error("cleanup did not detach consumer")
					}
					released.Add(1)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			})
			if _, err := c.PrepareLive(t.Context(), media.LiveRequest{ChannelID: liveID("2", "channel"), MaxFrameRate: 30, AudioIndex: test.index}); err == nil {
				t.Fatal("invalid audio selection succeeded")
			}
			if released.Load() != 1 || stopped.Load() != 1 {
				t.Fatal("failed selection leaked tuner or conversion")
			}
		})
	}
}

func TestLiveAudioCapabilityRequiresSelectableAlternatives(t *testing.T) {
	for _, test := range []struct {
		name, part string
		available  bool
	}{
		{"two tracks", `{"id":10,"Stream":[{"id":11,"streamType":2,"index":1},{"id":12,"streamType":2,"index":2}]}`, true},
		{"one track", `{"id":10,"Stream":[{"id":11,"streamType":2,"index":1}]}`, false},
		{"no part identity", `{"Stream":[{"id":11,"streamType":2,"index":1},{"id":12,"streamType":2,"index":2}]}`, false},
		{"no stream identity", `{"id":10,"Stream":[{"id":11,"streamType":2,"index":1},{"streamType":2,"index":2}]}`, false},
		{"duplicate index", `{"id":10,"Stream":[{"id":11,"streamType":2,"index":1},{"id":12,"streamType":2,"index":1}]}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := liveClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/tune"):
					fmt.Fprintf(w, `{"MediaContainer":{"Metadata":[{"Media":[{"uuid":"live-source","Part":[%s]}]}]}}`, test.part)
				case strings.HasSuffix(r.URL.Path, "/decision"):
					fmt.Fprint(w, `{"MediaContainer":{"generalDecisionCode":1001}}`)
				case strings.HasSuffix(r.URL.Path, "/stop"), strings.HasPrefix(r.URL.Path, "/media/grabbers/operations/"):
				default:
					t.Errorf("default audio made unexpected request %s", r.URL.Path)
				}
			})
			stream, err := c.PrepareLive(t.Context(), media.LiveRequest{ChannelID: liveID("2", "channel"), MaxFrameRate: 30, AudioIndex: -1})
			if err != nil {
				t.Fatal(err)
			}
			if stream.LiveAudio != test.available {
				t.Fatalf("LiveAudio = %v", stream.LiveAudio)
			}
			if err := stream.Release(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
