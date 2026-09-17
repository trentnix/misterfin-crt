package browser

import (
	"encoding/json"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"misterfin-crt/internal/diagnostics"
	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/media"
	"misterfin-crt/internal/rendering"
)

func TestOptionalArtworkFailuresAreLoggedWithoutBanner(t *testing.T) {
	for _, kind := range []string{"Primary", "Backdrop", "Logo", "cover", "covers", ""} {
		t.Run(kind, func(t *testing.T) {
			s := testSession(t)
			path := filepath.Join(t.TempDir(), "log")
			log, err := diagnostics.Open(diagnostics.Config{Enabled: true, Path: path, MaxBytes: 4096})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { log.Close() })
			s.config.Diagnostics = log
			cover := image.NewRGBA(image.Rect(0, 0, 2, 2))
			s.selection.current.artwork.Primary = cover
			result := selectionResult{update: selectionUpdate{kind: selectionArtwork, art: artUpdate{kind: kind}, err: errors.New("private image URL")}}
			if s.handleSelection(result) || s.selection.err != "" || s.selection.current.artwork.Primary != cover {
				t.Fatal("optional image failure changed visible state")
			}
			if err := log.Close(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var event map[string]any
			if err := json.Unmarshal(data, &event); err != nil {
				t.Fatal(err)
			}
			if event["msg"] != "browser.artwork" || event["kind"] != kind || event["failed"] != true || strings.Contains(string(data), "private") {
				t.Fatalf("incorrect or unsafe diagnostic: %s", data)
			}
		})
	}
}

func TestPhotoErrorClearsOnNavigationAndCannotReturn(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.model.Current().Page.Items = []media.Item{{ID: "library", CollectionType: "photos"}}
	s.model.Stack = append(s.model.Stack, View{Location: media.Location{Kind: "items"}, Page: media.Page{Items: []media.Item{{ID: "photo", Type: "Photo"}, {ID: "other", Type: "Photo"}}}})
	s.model.Key(control.Open)
	s.loadSelection()
	failed := selectionResult{generation: s.selection.generation, update: selectionUpdate{kind: selectionArtwork, art: artUpdate{kind: "Photo"}, err: errors.New("missing photo")}}
	if !s.handleSelection(failed) || s.selection.err == "" {
		t.Fatal("photo failure must remain visible")
	}
	// Back to the list must discard the error and reject a late worker result.
	s.handleKey(control.Back)
	if s.selection.err != "" || s.handleSelection(failed) {
		t.Fatal("photo error followed navigation to its list")
	}
	s.handleKey(control.Down)
	if s.selection.err != "" || s.selection.key != "other" {
		t.Fatal("error followed the next list selection")
	}
	// Returning to the carousel may retry the same image as a mosaic cover.
	s.model.ReturnToParent()
	s.loadSelection()
	failed.generation = s.selection.generation
	failed.update.art.kind = "cover"
	if s.handleSelection(failed) || s.selection.err != "" || s.selection.key != "root:library" {
		t.Fatal("mosaic recreated a child screen's error")
	}
}

func TestArtworkFallbackPreservesOtherErrors(t *testing.T) {
	for _, kind := range []selectionUpdateKind{selectionDetails, selectionCount} {
		s := testSession(t)
		s.handleSelection(selectionResult{update: selectionUpdate{kind: kind, err: errors.New("failed")}})
		previous := s.selection.err
		s.handleSelection(selectionResult{update: selectionUpdate{kind: selectionArtwork, art: artUpdate{kind: "Backdrop"}, err: errors.New("missing")}})
		if previous == "" || s.selection.err != previous {
			t.Fatal("optional artwork failure erased a metadata error")
		}
	}
	s := testSession(t)
	s.handleSelection(selectionResult{update: selectionUpdate{kind: selectionArtwork, art: artUpdate{kind: "cover"}, err: media.ErrUnauthorized}})
	if s.setup.Kind == rendering.SetupHidden {
		t.Fatal("artwork fallback hid authentication failure")
	}
}
