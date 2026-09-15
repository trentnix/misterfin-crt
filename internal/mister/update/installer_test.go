package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"misterfin-crt/internal/release"
	updateapi "misterfin-crt/internal/update"
)

func testARM() []byte {
	data := make([]byte, 64)
	copy(data, "\x7fELF\x01\x01")
	data[16], data[18] = 2, 40
	return data
}

func testPayload() map[string][]byte {
	return map[string][]byte{
		"misterfin-crt/misterfin-crt": testARM(), "misterfin-crt/mplayer-arm": testARM(),
		"Scripts/MiSTerFin-CRT.sh": []byte("#!/bin/sh\n"),
		"misterfin-crt/VERSION":    []byte("v0.2.0\n"), "misterfin-crt/UPDATE_FORMAT": []byte("1\n"),
		"misterfin-crt/BUILD.txt": []byte("build"), "misterfin-crt/LICENSE": []byte("license"),
		"misterfin-crt/THIRD_PARTY.md": []byte("notices"), "misterfin-crt/licenses/new.txt": []byte("new notice"),
		"misterfin-crt/settings.example.json": []byte("{}"),
	}
}

func testArchive(t *testing.T, payload map[string][]byte) []byte {
	t.Helper()
	var sums strings.Builder
	var names []string
	for name := range payload {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name != "SHA256SUMS" {
			fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(payload[name]), name)
		}
	}
	if payload["SHA256SUMS"] == nil {
		payload["SHA256SUMS"] = []byte(sums.String())
		names = append(names, "SHA256SUMS")
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0644)
		dest, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dest.Write(payload[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testInstaller(t *testing.T, archive []byte) (*Installer, map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	app := filepath.Join(dir, "misterfin-crt")
	launcher := filepath.Join(dir, "Scripts", "MiSTerFin-CRT.sh")
	original := map[string][]byte{
		filepath.Join(app, "misterfin-crt"):         []byte("old client"),
		filepath.Join(app, "mplayer-arm"):           []byte("old player"),
		launcher:                                    []byte("old launcher"),
		filepath.Join(app, "VERSION"):               []byte("v0.1.0\n"),
		filepath.Join(app, "settings.json"):         []byte("private settings"),
		filepath.Join(app, "jellyfin.conf"):         []byte("private server and token"),
		filepath.Join(app, "state", "session.json"): []byte("private sign-in"),
	}
	for path, data := range original {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0755); err != nil {
			t.Fatal(err)
		}
	}
	i := New(app, launcher)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
			t.Error("credentials sent to release server")
		}
		if strings.HasSuffix(r.URL.Path, "/SHA256SUMS") {
			fmt.Fprintf(w, "%x  misterfin-crt-v0.2.0-mister.zip\n", sha256.Sum256(archive))
		} else {
			w.Write(archive)
		}
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	transport := server.Client().Transport
	i.client.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
		request := r.Clone(r.Context())
		request.URL.Scheme, request.URL.Host = target.Scheme, target.Host
		return transport.RoundTrip(request)
	})
	return i, original
}

func available() release.Status {
	return release.Status{Latest: "v0.2.0", Available: true, HasBundle: true}
}

func assertOriginal(t *testing.T, original map[string][]byte) {
	t.Helper()
	for path, want := range original {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s was not preserved: %q, %v", path, got, err)
		}
	}
}

func TestInstallVerifiedPairPreservesState(t *testing.T) {
	payload := testPayload()
	i, original := testInstaller(t, testArchive(t, payload))
	var phases []updateapi.Phase
	err := i.Install(context.Background(), available(), func(p updateapi.Progress) {
		if len(phases) == 0 || phases[len(phases)-1] != p.Phase {
			phases = append(phases, p.Phase)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(phases, []updateapi.Phase{updateapi.Downloading, updateapi.Validating, updateapi.Installing}) {
		t.Fatal(phases)
	}
	for name, want := range payload {
		if name == "SHA256SUMS" {
			continue
		}
		path := i.destination(name)
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: %q, %v", name, got, err)
		}
		delete(original, path)
	}
	assertOriginal(t, original)
	restored, err := i.Recover()
	if err != nil || restored {
		t.Fatalf("completed update recovered: %v %v", restored, err)
	}
	if _, err := os.Stat(filepath.Join(i.root, pendingName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("journal left after success")
	}
}

func TestFailedReplacementRollsBackEveryPosition(t *testing.T) {
	for failAt := 1; failAt <= len(testPayload()); failAt++ {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			i, original := testInstaller(t, testArchive(t, testPayload()))
			calls := 0
			i.replace = func(dest, source string, mode os.FileMode) error {
				calls++
				if calls == failAt {
					return errors.New("injected write failure")
				}
				return replaceFile(dest, source, mode)
			}
			if err := i.Install(context.Background(), available(), nil); err == nil || errors.Is(err, updateapi.ErrRecovery) {
				t.Fatalf("rollback result: %v", err)
			}
			assertOriginal(t, original)
			if _, err := os.Stat(filepath.Join(i.root, "licenses/new.txt")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("new file survived rollback")
			}
		})
	}
}

func TestInterruptedInstallRecoversIdempotently(t *testing.T) {
	i, original := testInstaller(t, testArchive(t, testPayload()))
	calls := 0
	i.replace = func(dest, source string, mode os.FileMode) error {
		if err := replaceFile(dest, source, mode); err != nil {
			return err
		}
		calls++
		if calls == 4 {
			panic("simulated process interruption")
		}
		return nil
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("did not interrupt")
			}
		}()
		_ = i.Install(context.Background(), available(), nil)
	}()
	restored, err := i.Recover()
	if err != nil || !restored {
		t.Fatalf("recover: %v %v", restored, err)
	}
	assertOriginal(t, original)
	restored, err = i.Recover()
	if err != nil || restored {
		t.Fatalf("second recover: %v %v", restored, err)
	}
}

func TestCancellationDuringReplacementRollsBack(t *testing.T) {
	i, original := testInstaller(t, testArchive(t, testPayload()))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	i.replace = func(dest, source string, mode os.FileMode) error {
		err := replaceFile(dest, source, mode)
		cancel()
		return err
	}
	if err := i.Install(ctx, available(), nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	assertOriginal(t, original)
}

func TestInvalidArchivesLeaveInstallationUntouched(t *testing.T) {
	for _, kind := range []string{"legacy", "format", "version", "missing-player", "host-player", "traversal", "active-config", "checksum"} {
		t.Run(kind, func(t *testing.T) {
			payload := testPayload()
			switch kind {
			case "legacy":
				delete(payload, "misterfin-crt/UPDATE_FORMAT")
			case "format":
				payload["misterfin-crt/UPDATE_FORMAT"] = []byte("2\n")
			case "version":
				payload["misterfin-crt/VERSION"] = []byte("v9.0.0\n")
			case "missing-player":
				delete(payload, "misterfin-crt/mplayer-arm")
			case "host-player":
				payload["misterfin-crt/mplayer-arm"] = []byte("wrong executable")
			case "traversal":
				payload["misterfin-crt/licenses/../../outside"] = []byte("bad")
			case "active-config":
				payload["misterfin-crt/settings.json"] = []byte("bad")
			case "checksum":
				payload["SHA256SUMS"] = []byte("bad checksum list")
			}
			i, original := testInstaller(t, testArchive(t, payload))
			if err := i.Install(context.Background(), available(), nil); err == nil {
				t.Fatal("accepted invalid archive")
			}
			assertOriginal(t, original)
		})
	}
}

func TestChecksumFailureAndCanceledDownload(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) {
			i, original := testInstaller(t, testArchive(t, testPayload()))
			i.client.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
				if cancel {
					return nil, context.Canceled
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("0", 64) + "  misterfin-crt-v0.2.0-mister.zip\n")), Header: make(http.Header)}, nil
			})
			if err := i.Install(context.Background(), available(), nil); err == nil {
				t.Fatal("accepted damaged download")
			}
			assertOriginal(t, original)
		})
	}
}

func TestConcurrentInstallAndUnsafeDestinations(t *testing.T) {
	i, original := testInstaller(t, testArchive(t, testPayload()))
	unlock, err := i.lock()
	if err != nil {
		t.Fatal(err)
	}
	if err := i.Install(context.Background(), available(), nil); err == nil {
		t.Fatal("overlapping install")
	}
	unlock()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(i.root, "licenses")); err != nil {
		t.Fatal(err)
	}
	if err := i.Install(context.Background(), available(), nil); err == nil {
		t.Fatal("followed license symlink")
	}
	assertOriginal(t, original)
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("wrote outside installation")
	}
}

func TestRedirectPolicyAndUnsafeVersion(t *testing.T) {
	client := downloadClient()
	for _, endpoint := range []string{"http://github.com/x", "https://example.com/x", "https://github.com:444/x", "https://user@github.com/x"} {
		req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
		if client.CheckRedirect(req, nil) == nil {
			t.Fatalf("accepted %s", endpoint)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, "https://release-assets.githubusercontent.com/x", nil)
	if err := client.CheckRedirect(req, nil); err != nil {
		t.Fatal(err)
	}
	i, original := testInstaller(t, testArchive(t, testPayload()))
	status := available()
	status.Latest = "../../other"
	if err := i.Install(context.Background(), status, nil); !errors.Is(err, updateapi.ErrManual) {
		t.Fatal(err)
	}
	assertOriginal(t, original)
}

func TestCommittedTransactionKeepsNewFilesAfterInterruption(t *testing.T) {
	payload := testPayload()
	count := len(payload)
	i, _ := testInstaller(t, testArchive(t, payload))
	calls := 0
	i.replace = func(dest, source string, mode os.FileMode) error {
		if err := replaceFile(dest, source, mode); err != nil {
			return err
		}
		calls++
		if calls == count {
			pending := filepath.Join(i.root, pendingName)
			if err := durableWrite(filepath.Join(pending, "committed"), []byte("1\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := syncDir(pending); err != nil {
				t.Fatal(err)
			}
			panic("interrupted after commit")
		}
		return nil
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("did not interrupt")
			}
		}()
		_ = i.Install(context.Background(), available(), nil)
	}()
	restored, err := i.Recover()
	if err != nil || restored {
		t.Fatalf("committed recovery: %v %v", restored, err)
	}
	for name, want := range payload {
		if name == "SHA256SUMS" {
			continue
		}
		got, err := os.ReadFile(i.destination(name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("committed file changed: %s", name)
		}
	}
}

func TestRecoveryRejectsInvalidJournalAndRetainsEvidence(t *testing.T) {
	i, original := testInstaller(t, testArchive(t, testPayload()))
	pending := filepath.Join(i.root, pendingName)
	if err := os.Mkdir(pending, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pending, "journal.json"), []byte(`[{"Name":"misterfin-crt/state/session.json","HadOriginal":false}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Recover(); !errors.Is(err, updateapi.ErrRecovery) {
		t.Fatal(err)
	}
	assertOriginal(t, original)
	if _, err := os.Stat(pending); err != nil {
		t.Fatal("removed invalid journal")
	}
}

func TestRollbackPreservesOriginalPermissions(t *testing.T) {
	i, original := testInstaller(t, testArchive(t, testPayload()))
	path := filepath.Join(i.root, "VERSION")
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	i.replace = func(dest, source string, mode os.FileMode) error { return errors.New("write failure") }
	if err := i.Install(context.Background(), available(), nil); err == nil {
		t.Fatal("expected failure")
	}
	after, err := os.Stat(path)
	if err != nil || before.Mode().Perm() != after.Mode().Perm() {
		t.Fatal("permissions changed during rollback")
	}
	assertOriginal(t, original)
}

// A pending transaction can appear after startup if another client is killed.
// Even successful rollback requires a restart before the running pair is used.
func TestInstallRequiresRestartAfterRecoveringAnotherProcess(t *testing.T) {
	i, original := testInstaller(t, testArchive(t, testPayload()))
	i.replace = func(dest, source string, mode os.FileMode) error {
		if err := replaceFile(dest, source, mode); err != nil {
			return err
		}
		panic("interrupted update")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("did not interrupt")
			}
		}()
		_ = i.Install(context.Background(), available(), nil)
	}()
	i.replace = replaceFile
	if err := i.Install(context.Background(), available(), nil); !errors.Is(err, updateapi.ErrRecovery) {
		t.Fatal(err)
	}
	assertOriginal(t, original)
	restored, err := i.Recover()
	if err != nil || restored {
		t.Fatalf("already recovered: %v %v", restored, err)
	}
}
