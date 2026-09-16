package media_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Shared packages must not import the server adapter. Check direct imports in
// each shared layer and its helpers so failures identify the responsible file.
// Adapter integration tests remain permitted.
func TestSharedPackagesDoNotImportProviders(t *testing.T) {
	for _, dir := range []string{"media", "connection", "browser", "artwork", "playback", "player", "rendering", "videoout"} {
		err := filepath.WalkDir(filepath.Join("..", dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range file.Imports {
				name, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				if name == "misterfin-crt/internal/jellyfin" || strings.HasPrefix(name, "misterfin-crt/internal/jellyfin/") || name == "misterfin-crt/internal/plex" || strings.HasPrefix(name, "misterfin-crt/internal/plex/") {
					t.Errorf("%s imports server implementation %s", path, name)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
