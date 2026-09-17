package displaymode

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreConfigPreservesOtherModes(t *testing.T) {
	for _, tc := range []struct{ name, setting, want string }{
		{"rgb", "ypbpr=0", "forced_scandoubler=1"},
		{"component", "ypbpr=1", "forced_scandoubler=0"},
		{"component-name", "vga_mode=ypbpr", "forced_scandoubler=0"},
		{"new-key-precedence", "ypbpr=1\nvga_mode=rgb", "forced_scandoubler=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := []byte("[MiSTer]\r\n" + tc.setting + "\r\ncomposite_sync=1\r\n[Menu]\r\nvideo_mode=640,240,60\r\n[NeoGeo]\r\nforced_scandoubler=0\r\n")
			got, err := CoreConfig(original)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(got, original) {
				t.Fatal("existing configuration changed")
			}
			if !strings.Contains(string(got), "[MiSTerFinInterlaced]\ndirect_video=1\n"+tc.want) {
				t.Fatalf("wrong mode block: %s", got)
			}
			managed := string(got[len(original):])
			if !strings.Contains(managed, "\nlog_file_entry=1\n") {
				t.Fatal("interlaced core does not publish menu readiness")
			}
			again, err := CoreConfig(got)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(again, got) {
				t.Fatal("repeated launches changed configuration")
			}
		})
	}
}

func TestCoreConfigRefusesUnmanagedOrIncompleteSection(t *testing.T) {
	for _, text := range []string{"[MiSTerFinInterlaced]\ncustom=1\n", blockStart + "\n[MiSTerFinInterlaced]\n"} {
		if _, err := CoreConfig([]byte(text)); err == nil {
			t.Fatal("overwrote existing settings")
		}
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "display.json")
	if c, err := Load(path); err != nil || c.Interlaced {
		t.Fatalf("default: %+v %v", c, err)
	}
	for _, tc := range []struct {
		data           string
		enabled, valid bool
	}{
		{`{"interlaced":true}`, true, true}, {`{"interlaced":false}`, false, true},
		{strings.Repeat(" ", 4097), false, false}, {`{}`, false, true}, {`null`, false, false}, {`{"interlcaed":true}`, false, false},
		{`{"interlaced":"true"}`, false, false}, {`{} {}`, false, false},
	} {
		if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		if (err == nil) != tc.valid || c.Interlaced != tc.enabled {
			t.Fatalf("%s: %+v %v", tc.data, c, err)
		}
	}
}

func TestOwnedProcessesMatchWholeMarker(t *testing.T) {
	root := t.TempDir()
	for pid, value := range map[string]string{"10": "MISTERFIN_CRT_DISPLAY_OWNER=123", "11": "OTHER=MISTERFIN_CRT_DISPLAY_OWNER=123", "12": "MISTERFIN_CRT_DISPLAY_OWNER=1234"} {
		dir := filepath.Join(root, pid)
		os.Mkdir(dir, 0700)
		os.WriteFile(filepath.Join(dir, "environ"), []byte("BEFORE=1\x00"+value+"\x00AFTER=2\x00"), 0600)
	}
	got := ownedProcesses(root, "123")
	if len(got) != 1 || got[0] != 10 {
		t.Fatalf("owned processes: %v", got)
	}
}

func TestCoreConfigRejectsUnverifiedEncoderModes(t *testing.T) {
	if _, err := CoreConfig([]byte("[MiSTer]\nvga_mode=cvbs\n")); err == nil {
		t.Fatal("accepted unverified encoder mode")
	}
}
