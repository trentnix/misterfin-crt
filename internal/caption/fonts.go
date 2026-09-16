package caption

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
)

// fontArchive contains original, unmodified Noto fonts and their licenses.
// tools/build_subtitle_fonts.py records pinned sources and checksums inside it.
//
//go:embed fonts.zip
var fontArchive []byte

// fontSet selects by script before falling back to Latin and symbols. Font faces
// have mutable shaping caches, so each renderer owns its set. Only faces needed
// by a cue are parsed, keeping ordinary Latin captions cheap.
type fontSet struct {
	files   map[string]*zip.File
	scripts map[language.Script]string
	faces   map[string]*font.Face
	script  language.Script
}

// init indexes trusted embedded resources without parsing all the fonts.
func (f *fontSet) init() {
	if f.files != nil {
		return
	}
	archive, err := zip.NewReader(bytes.NewReader(fontArchive), int64(len(fontArchive)))
	if err != nil {
		panic(fmt.Errorf("embedded subtitle fonts: %w", err))
	}
	f.files = make(map[string]*zip.File)
	f.faces = make(map[string]*font.Face)
	f.scripts = make(map[language.Script]string)
	for _, entry := range archive.File {
		f.files[entry.Name] = entry
	}
	var manifest []struct {
		File    string
		Scripts []string
	}
	if err := json.Unmarshal(f.read("manifest.json"), &manifest); err != nil {
		panic(err)
	}
	for _, entry := range manifest {
		for _, tag := range entry.Scripts {
			script, err := language.ParseScript(tag)
			if err != nil {
				panic(err)
			}
			f.scripts[script] = entry.File
		}
	}
}

// read loads a build-time resource. Failure means a broken binary, not bad user
// input. Tests validate every bundled font and its recorded checksum.
func (f *fontSet) read(name string) []byte {
	file := f.files[name]
	if file == nil {
		panic("missing embedded subtitle resource: " + name)
	}
	if file.Method != zip.Store {
		panic("embedded subtitle fonts must be stored uncompressed")
	}
	offset, err := file.DataOffset()
	if err != nil {
		panic(err)
	}
	end := offset + int64(file.UncompressedSize64)
	if offset < 0 || end < offset || end > int64(len(fontArchive)) {
		panic("invalid embedded subtitle font bounds")
	}
	// Borrow the immutable embedded bytes. Decompression and an extra font-sized
	// allocation would stall the first CJK caption on the ARM CPU.
	return fontArchive[offset:end]
}

// face parses each face at most once for the lifetime of this renderer.
func (f *fontSet) face(name string) *font.Face {
	if face := f.faces[name]; face != nil {
		return face
	}
	face, err := font.ParseTTF(bytes.NewReader(f.read(name)))
	if err != nil {
		panic(fmt.Errorf("embedded subtitle font %s: %w", name, err))
	}
	f.faces[name] = face
	return face
}

// SetScript implements shaping.FontmapScript, keeping shared punctuation and
// combining marks in the surrounding script's face whenever possible.
func (f *fontSet) SetScript(script language.Script) { f.script = script }

// ResolveFace implements shaping.Fontmap. Missing glyphs use Noto's visible
// replacement box instead of silently replacing source text with question marks.
func (f *fontSet) ResolveFace(r rune) *font.Face {
	if name := f.scripts[f.script]; name != "" {
		face := f.face(name)
		if _, ok := face.NominalGlyph(r); ok {
			return face
		}
	}
	base := f.face("NotoSans-Regular.ttf")
	if _, ok := base.NominalGlyph(r); ok {
		return base
	}
	for _, name := range []string{"NotoSansSymbols-Regular.ttf", "NotoSansSymbols2-Regular.ttf"} {
		symbols := f.face(name)
		if _, ok := symbols.NominalGlyph(r); ok {
			return symbols
		}
	}
	return base
}
