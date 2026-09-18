package playback

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mistervision/internal/jellyfin"
	"mistervision/internal/media"
	nativeplayer "mistervision/internal/player/mplayer"
)

func TestAudioMeterLifetime(t *testing.T) {
	for _, failPreparation := range []bool{false, true} {
		name := "decoder exit"
		if failPreparation {
			name = "preparation failure"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			player := filepath.Join(dir, "player")
			// Check that the decoder can still open its export file, then emit
			// progress so normal reporting and end-of-playback cleanup run.
			script := `#!/bin/sh
for arg do
    case "$arg" in
        *,export=*)
            meter=${arg#*,export=}
            meter=${meter%:512}
            test -f "$meter" || exit 2
            printf 'ANS_TIME_POSITION=1\n'
            exit 0
            ;;
    esac
done
exit 3
`
			if err := os.WriteFile(player, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			allocated := make(chan string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/Items/song" {
					files, _ := filepath.Glob(filepath.Join(dir, "mistervision-audio-*"))
					if len(files) != 1 {
						t.Errorf("preparation found %d meters, want one", len(files))
					} else {
						allocated <- files[0]
					}
					if failPreparation {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					fmt.Fprint(w, `{"Id":"song","Type":"Audio","RunTimeTicks":900000000}`)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{UserID: "user"})
			config := Config{AudioDecoder: nativeplayer.Decoder{Player: player, Width: 640, Height: 240}, VideoDecoder: nativeplayer.Decoder{Width: 640, Height: 240}}
			lastMeter := ""
			for range 2 {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := Run(ctx, client, config, Request{Item: media.Item{ID: "song", Type: "Audio"}, Callbacks: Callbacks{Levels: func(AudioLevels) {}}})
				cancel()
				if (err != nil) != failPreparation {
					t.Fatalf("Run error = %v, preparation failure = %t", err, failPreparation)
				}
				select {
				case meter := <-allocated:
					if meter == lastMeter {
						t.Fatal("reused the preceding request's meter file")
					}
					lastMeter = meter
				default:
					t.Fatal("no meter allocated")
				}
				files, err := filepath.Glob(filepath.Join(dir, "mistervision-audio-*"))
				if err != nil || len(files) != 0 {
					t.Fatalf("meter files left after Run: %v (%v)", files, err)
				}
			}
		})
	}
}
