// Package release identifies the running build and checks published releases.
// It never downloads or installs an update.
package release

import (
	"runtime/debug"
	"strings"
)

// Version is set at build time with -ldflags. Release builds use vMAJOR.MINOR.PATCH.
// Untagged builds retain dev and display the revision recorded by the Go toolchain.
var Version = "dev"

// Build identifies the installed executable independently of update availability.
type Build struct {
	Version  string
	Revision string
	Modified bool
}

// CurrentBuild reads compiler-provided VCS metadata. Missing metadata is valid
// for builds made outside a Git checkout.
func CurrentBuild() Build {
	b := Build{Version: Version}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				b.Revision = s.Value
			case "vcs.modified":
				b.Modified = s.Value == "true"
			}
		}
	}
	return b
}

// String returns a compact version and revision suitable for the CRT.
func (b Build) String() string {
	v := b.Version
	if v == "" {
		v = "dev"
	}
	if b.Revision != "" {
		v += " (" + b.Revision[:min(7, len(b.Revision))] + ")"
	}
	if b.Modified {
		v += " modified"
	}
	return v
}

// stableParts accepts complete stable semantic versions, with optional v prefix
// and build metadata. Decimal strings avoid overflow for large version numbers.
func stableParts(v string) ([]string, bool) {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		for _, part := range strings.Split(v[i+1:], ".") {
			if part == "" {
				return nil, false
			}
			for _, c := range part {
				if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-') {
					return nil, false
				}
			}
		}
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil, false
	}
	for _, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return nil, false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return nil, false
			}
		}
	}
	return parts, true
}

// newer compares stable releases numerically. Development builds have no release
// ordering, so a published release is offered without claiming it is newer.
func newer(latest, installed string) bool {
	a, ok := stableParts(latest)
	if !ok {
		return false
	}
	base, _, prerelease := strings.Cut(strings.SplitN(installed, "+", 2)[0], "-")
	b, ok := stableParts(base)
	if !ok {
		return true
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return len(a[i]) > len(b[i])
		}
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return prerelease
}
