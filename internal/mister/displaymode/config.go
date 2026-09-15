// Package displaymode owns the optional interlaced MiSTer core for one app run.
// It switches hardware before the framebuffer opens and restores the normal
// menu after the child exits, including when the child crashes.
package displaymode

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"misterfin-crt/internal/settings"
)

// Config selects the native output at startup. The default preserves the
// current display. Interlaced requires the separately installed community core.
type Config struct {
	// Interlaced loads the supported standalone core for this application run.
	Interlaced bool `json:"interlaced"`
}

// Load reads legacy display settings. A missing file preserves the current mode.
func Load(path string) (Config, error) { return Parse(settings.Read(path, 4096, false)) }

// Parse validates the display section before any hardware changes occur.
func Parse(source settings.Section) (Config, error) {
	var c Config
	if err := source.Decode(&c); err != nil {
		return c, fmt.Errorf("display configuration %s: %w", source.Path, err)
	}
	return c, nil
}

const coreName = "MiSTerFinInterlaced"
const blockStart = "; BEGIN MiSTerFin CRT interlaced output"
const blockEnd = "; END MiSTerFin CRT interlaced output"

// CoreConfig preserves existing INI text and adds an isolated MGL-name section.
// The section is inert for the ordinary Menu core and other applications.
// RGB and component inherit their existing color/sync settings unchanged.
func CoreConfig(data []byte) ([]byte, error) {
	text := string(data)
	if start := strings.Index(text, blockStart); start >= 0 {
		end := strings.Index(text[start:], blockEnd)
		if end < 0 {
			return nil, errors.New("incomplete MiSTerFin interlaced configuration block")
		}
		end += start + len(blockEnd)
		if end < len(text) && text[end] == '\n' {
			end++
		}
		text = text[:start] + text[end:]
	}
	if strings.Contains(strings.ToLower(text), "["+strings.ToLower(coreName)+"]") {
		return nil, errors.New("MiSTerFinInterlaced INI section already exists outside the managed block")
	}
	values := map[string]string{}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, ";", 2)[0])
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(strings.TrimSpace(strings.Trim(line, "[]")))
			continue
		}
		if section != "mister" && section != "menu" {
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			values[strings.ToLower(strings.TrimSpace(key))] = strings.ToLower(strings.TrimSpace(value))
		}
	}
	scandoubler := 1
	component := values["ypbpr"] == "1"
	if mode := values["vga_mode"]; mode != "" {
		if mode != "rgb" && mode != "ypbpr" {
			return nil, errors.New("interlaced output currently supports RGB or component vga_mode")
		}
		component = mode == "ypbpr"
	}
	if component {
		scandoubler = 0
	}
	block := fmt.Sprintf("%s\n[%s]\ndirect_video=1\nforced_scandoubler=%d\nfb_size=1\nfb_terminal=1\n%s\n", blockStart, coreName, scandoubler, blockEnd)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return []byte(text + block), nil
}

// prepareCore verifies the bitstream, preserves existing INI settings, and
// writes the launch descriptor. The returned path is ready for Main to load.
func prepareCore(directory string) (string, error) {
	const root = "/media/fat"
	core := filepath.Join(directory, "InterlacedMenu.rbf")
	info, statErr := os.Stat(core)
	if statErr != nil {
		return "", fmt.Errorf("install standalone InterlacedMenu.rbf v0.0.1 at %s: %w", core, statErr)
	}
	if info.Size() != 2489556 {
		return "", errors.New("unsupported interlaced core: install InterlacedMenu.rbf v0.0.1")
	}
	bitstream, readErr := os.ReadFile(core)
	if readErr != nil {
		return "", readErr
	}
	if fmt.Sprintf("%x", sha256.Sum256(bitstream)) != "0158e0338a00441271f38be0703c22253d53ea39b60a1a96b7ec964bedae8999" {
		return "", errors.New("interlaced core checksum does not match v0.0.1")
	}
	original, err := os.ReadFile(root + "/MiSTer.ini")
	if err != nil {
		return "", err
	}
	configured, err := CoreConfig(original)
	if err != nil {
		return "", err
	}
	if string(configured) != string(original) {
		backup := root + "/misterfin-crt/MiSTer.ini.before-interlaced"
		if _, e := os.Stat(backup); errors.Is(e, os.ErrNotExist) {
			if e = os.WriteFile(backup, original, 0600); e != nil {
				return "", e
			}
		}
		if err := replace(root+"/MiSTer.ini", configured); err != nil {
			return "", err
		}
	}
	// MGL resolves RBF paths relative to the SD card root, not the descriptor.
	relative, err := filepath.Rel(root, core)
	if err != nil || strings.HasPrefix(relative, "..") || strings.ContainsAny(relative, "<&>\r\n") {
		return "", errors.New("interlaced core must be inside /media/fat with an XML-safe path")
	}
	mgl := filepath.Join(directory, "Interlaced.mgl")
	xml := "<mistergamedescription><rbf>" + strings.TrimSuffix(relative, ".rbf") + "</rbf><setname>" + coreName + "</setname></mistergamedescription>\n"
	if err := replace(mgl, []byte(xml)); err != nil {
		return "", err
	}
	return mgl, nil
}

// replace publishes a complete file before it can be read during a core load.
func replace(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".interlaced-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
