package update

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"mistervision/internal/release"
)

// TestDownloadedRelease installs the actual release ZIP into temporary storage.
// It also interrupts replacement and verifies recovery without executing binaries
// or touching the installed application. Normal test runs need no release asset.
func TestDownloadedRelease(t *testing.T) {
	archivePath := os.Getenv("MISTERVISION_VERIFY_ARCHIVE")
	if archivePath == "" {
		t.Skip("set MISTERVISION_VERIFY_ARCHIVE to verify downloaded assets")
	}
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	file, err := reader.Open("mistervision/VERSION")
	if err != nil {
		t.Fatal(err)
	}
	label, err := io.ReadAll(io.LimitReader(file, 100))
	file.Close()
	if err != nil || len(label) == 0 || label[len(label)-1] != '\n' {
		t.Fatal("invalid version file")
	}
	version := string(label[:len(label)-1])
	status := release.Status{Latest: version, Available: true, HasBundle: true}
	t.Run("install", func(t *testing.T) {
		installer, original := testInstallerVersion(t, archive, version)
		if err := installer.Install(t.Context(), status, nil); err != nil {
			t.Fatal(err)
		}
		for _, entry := range reader.File {
			if entry.Name == "SHA256SUMS" {
				continue
			}
			source, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			want, err := io.ReadAll(source)
			source.Close()
			if err != nil {
				t.Fatal(err)
			}
			destination := installer.destination(entry.Name)
			got, err := os.ReadFile(destination)
			if err != nil || !bytes.Equal(want, got) {
				t.Fatalf("installed contents differ: %s", entry.Name)
			}
			delete(original, destination)
		}
		assertOriginal(t, original)
		if restored, err := installer.Recover(); restored || err != nil {
			t.Fatalf("unexpected recovery: %t %v", restored, err)
		}
	})
	t.Run("interrupted-replacement", func(t *testing.T) {
		installer, original := testInstallerVersion(t, archive, version)
		interruption := errors.New("simulated interruption")
		installer.replace = func(dest, source string, mode os.FileMode) error {
			if err := replaceFile(dest, source, mode); err != nil {
				return err
			}
			if dest == installer.destination("mistervision/mistervision") {
				panic(interruption)
			}
			return nil
		}
		func() {
			defer func() {
				if got := recover(); got != interruption {
					t.Fatalf("expected interruption, got %v", got)
				}
			}()
			_ = installer.Install(t.Context(), status, nil)
		}()
		if restored, err := installer.Recover(); !restored || err != nil {
			t.Fatalf("recovery failed: %t %v", restored, err)
		}
		assertOriginal(t, original)
		if restored, err := installer.Recover(); restored || err != nil {
			t.Fatalf("recovery not idempotent: %t %v", restored, err)
		}
	})
}
