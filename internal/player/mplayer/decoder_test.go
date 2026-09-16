package mplayer

import (
	"encoding/json"
	"fmt"
	"misterfin-crt/internal/jellyfin"
	"strings"
	"testing"
)

func TestMPlayerCRTAspect(t *testing.T) {
	var item jellyfin.Item
	json.Unmarshal([]byte(`{"MediaStreams":[{"Type":"Video","Width":720,"Height":576,"AspectRatio":"16:9"}]}`), &item)
	for _, h := range []int{240, 288} {
		args := Decoder{Width: 640, Height: h, Device: "/dev/fb0"}.Args(item, "")
		want := fmt.Sprintf("misterfin=640:%d:1.777777778:0", h)
		if !strings.Contains(strings.Join(args, " "), want) {
			t.Fatalf("args %v", args)
		}
	}
}

func TestLiveTVAspectFallbackAndMetadata(t *testing.T) {
	for _, tc := range []struct {
		aspect string
		dar    float64
	}{{"", 16.0 / 9}, {"16:9", 16.0 / 9}, {"4:3", 4.0 / 3}} {
		item := jellyfin.Item{Type: "TvChannel"}
		if tc.aspect != "" {
			item.MediaStreams = []jellyfin.MediaStream{{Type: "Video", Width: 720, Height: 576, AspectRatio: tc.aspect}}
		}
		args := Decoder{Width: 640, Height: 240, Device: "/dev/fb0"}.Args(item, "")
		want := fmt.Sprintf("misterfin=640:240:%.9f:0", tc.dar)
		if !strings.Contains(strings.Join(args, " "), want) {
			t.Fatalf("aspect %q: %v", tc.aspect, args)
		}
	}
}

func TestHardwareVideoSynchronization(t *testing.T) {
	for _, tc := range []struct{ kind, autosync, cacheMinimum string }{
		{"Movie", "30", "20"}, {"Episode", "30", "20"}, {"TvChannel", "1", "0"}, {"LiveTvChannel", "1", "0"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			args := Decoder{Width: 640, Height: 240, Device: "/dev/fb0"}.Args(jellyfin.Item{Type: tc.kind}, "")
			joined := " " + strings.Join(args, " ") + " "
			if !strings.Contains(joined, " -framedrop ") || !strings.Contains(joined, " -autosync "+tc.autosync+" ") {
				t.Fatalf("missing hardware synchronization policy: %v", args)
			}
			if !strings.Contains(joined, " -cache 8192 ") || !strings.Contains(joined, " -cache-min "+tc.cacheMinimum+" ") {
				t.Fatalf("incorrect startup buffering policy: %v", args)
			}
			if strings.Contains(joined, " -fps ") || strings.Contains(joined, " -speed ") {
				t.Fatalf("hardware playback must respect stream timing: %v", args)
			}
		})
	}
}
