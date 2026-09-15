package artwork

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheStagingPublishesCompleteFiles(t *testing.T) {
	c := cacheFiles{dir: filepath.Join(t.TempDir(), "cache")}
	staged, err := c.stage([]byte("old"))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(staged)
	if err := c.publish(staged, "image"); err != nil {
		t.Fatal(err)
	}
	replacement := bytes.Repeat([]byte("new"), 4096)
	staged, err = c.stage(replacement)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(staged)
	if got := c.read("image", 1, 20000); string(got) != "old" {
		t.Fatal("staging replaced current file")
	}
	info, err := os.Stat(staged)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("staged cache is not private", err)
	}
	if err := c.publish(staged, "image"); err != nil {
		t.Fatal(err)
	}
	if got := c.read("image", 1, 20000); !bytes.Equal(got, replacement) {
		t.Fatal("publication exposed incomplete pixels")
	}
	for _, bounds := range [][2]int64{{1, 2}, {20000, 30000}} {
		if c.read("image", bounds[0], bounds[1]) != nil {
			t.Fatal("file size bounds ignored")
		}
	}
	if err := os.Mkdir(filepath.Join(c.dir, "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	if c.read("directory", 0, 1<<20) != nil || c.read("missing", 0, 1<<20) != nil {
		t.Fatal("non-file read accepted")
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil || len(entries) != 2 {
		t.Fatal("publication left temporary files", err)
	}
}

func TestCacheInventoryTracksReplacementRemovalAndProtectedFiles(t *testing.T) {
	c := cacheFiles{dir: t.TempDir(), suffix: ".rgba", maxEntries: 2, maxBytes: 8}
	save := func(name, contents string) {
		t.Helper()
		staged, err := c.stage([]byte(contents))
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(staged)
		if err := c.publish(staged, name); err != nil {
			t.Fatal(err)
		}
		c.written(name, len(contents))
		c.prune(name)
	}
	if err := os.WriteFile(filepath.Join(c.dir, "unrelated"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	save("a.rgba", "1111")
	save("b.rgba", "2222")
	save("b.rgba", "22222222")
	if _, ok := c.files["a.rgba"]; ok {
		t.Fatal("replacement bytes not counted")
	}
	if c.files["b.rgba"].size != 8 {
		t.Fatal("current file removed or wrong size")
	}
	if _, err := os.Stat(filepath.Join(c.dir, "unrelated")); err != nil {
		t.Fatal("unrelated file removed")
	}
	// Removal must clear stale inventory if the file is already gone.
	if err := os.Remove(filepath.Join(c.dir, "b.rgba")); err != nil {
		t.Fatal(err)
	}
	if !c.remove("b.rgba") || len(c.files) != 0 {
		t.Fatal("removed file remains in budget")
	}
}
