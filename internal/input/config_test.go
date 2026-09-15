package input

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigPathsAndValidation(t *testing.T) {
	dir := t.TempDir()
	jellyfin := filepath.Join(dir, "jellyfin.conf")
	if c, err := LoadConfig("", jellyfin); err != nil || len(c.Profiles) != 0 {
		t.Fatalf("missing optional config: %+v, %v", c, err)
	}
	path := filepath.Join(dir, "input.json")
	if _, err := LoadConfig(path, jellyfin); err == nil {
		t.Fatal("missing explicit config accepted")
	}
	cases := []struct {
		data  string
		valid bool
	}{
		{`{"profiles":[{"match":"*Controller*","buttons":{"307":"select","310":""},"axes":{"2":{"rest":"minimum","positive":"seek-backward"},"0":{"negative":"previous","positive":"next","press":40,"release":20}}}]}`, true},
		{`{"profiles":[{"match":"*","buttons":{"1":"seek-forwards"}}]}`, false},
		{`{"profiles":[{"match":"["}]}`, false},
		{`{"profiles":[{"match":""}]}`, false},
		{`{"profiles":[{"match":"*","butons":{}}]}`, false},
		{`{"profiles":[{"match":"*","buttons":{"9999":"open"}}]}`, false},
		{`{"profiles":[{"match":"*","axes":{"64":{}}}]}`, false},
		{`{"profiles":[{"match":"*","axes":{"0":{"rest":"min"}}}]}`, false},
		{`{"profiles":[{"match":"*","axes":{"0":{"rest":"minimum","negative":"back"}}}]}`, false},
		{`{"profiles":[{"match":"*","axes":{"0":{"press":10,"release":15}}}]}`, false},
		{`{"profiles":[{"match":"*","axes":{"0":{"release":-1}}}]}`, false},
		{`{"profiles":[{"match":"Pad","button_labels":{"310":"L1"},"axis_labels":{"5":{"positive":"R2"}}}]}`, true},
		{`{"profiles":[{"match":"Pad","button_labels":{"310":"Label much too long"}}]}`, false},
		{`{"profiles":[{"match":"Pad","axis_labels":{"5":{"positive":"line\nline"}}}]}`, false},
		{strings.Repeat(" ", 65537), false}, {`{} {}`, false}, {`{} garbage`, false}, {`null`, false}, {``, false},
	}
	for _, tc := range cases {
		if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadConfig("", jellyfin)
		if (err == nil) != tc.valid {
			t.Errorf("%s: %v", tc.data, err)
		}
		if err != nil && !strings.Contains(err.Error(), path) {
			t.Errorf("missing path: %v", err)
		}
	}
}
