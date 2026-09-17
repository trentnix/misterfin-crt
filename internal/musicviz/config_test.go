package musicviz

import (
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"testing"

	"mistervision/internal/settings"
	"time"

	"mistervision/internal/ui"
)

func TestConfigAndAnimationAssets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "music.json")
	palette := color.Palette{color.Black, color.White}
	a, b := image.NewPaletted(image.Rect(0, 0, 2, 2), palette), image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	b.SetColorIndex(1, 1, 1)
	f, _ := os.Create(filepath.Join(dir, "custom.gif"))
	err := gif.EncodeAll(f, &gif.GIF{Image: []*image.Paletted{a, b}, Delay: []int{10, 20}})
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	config := `{"default":"Custom","meters":false,"backgrounds":[{"name":"Custom","type":"image","files":["custom.gif"]}]}`
	os.WriteFile(path, []byte(config), 0600)
	l, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p := l.Config.Backgrounds[0]
	if l.Config.Meters || len(p.frames) != 2 {
		t.Fatalf("bad configuration: %+v", l.Config)
	}
	if assetFrame(p, 150*time.Millisecond) != p.frames[1] || assetFrame(p, 350*time.Millisecond) != p.frames[0] {
		t.Fatal("GIF frame delays or looping lost")
	}
	for _, invalid := range []string{`{"default":"Missing"}`, `{"extra":true}`, `{"backgrounds":[]}`, `{"backgrounds":[{"name":"Bad","type":"rain","speed":-1}]}`} {
		os.WriteFile(path, []byte(invalid), 0600)
		if _, err := Load(path); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
}

func TestEffectsAndMeterPause(t *testing.T) {
	config := Defaults()
	config.Backgrounds = config.Backgrounds[:5]
	for i := range config.Backgrounds {
		p := &config.Backgrounds[i]
		p.Speed = 1
		p.Density = 40
		p.color = 0x80bfff
		v := .7
		p.Intensity = &v
	}
	l := &Library{Config: config}
	c := ui.New(640, 240)
	now := time.Now()
	art := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range art.Pix {
		art.Pix[i] = 255
	}
	for i, p := range config.Backgrounds {
		t.Run(p.Name, func(t *testing.T) {
			var r Renderer
			frame := Frame{Now: now, Levels: [2]float64{.5, .1}, Artwork: art}
			r.Draw(c, l, i, frame)
			frame.Now = now.Add(time.Second / 30)
			r.Draw(c, l, i, frame)
			if r.levels[0] <= r.levels[1] {
				t.Fatal("stereo levels lost")
			}
			before := r.levels[0]
			frame.Paused = true
			frame.Now = frame.Now.Add(100 * time.Millisecond)
			r.Draw(c, l, i, frame)
			if r.levels[0] >= before {
				t.Fatal("paused meters did not decay")
			}
			r.Meters(c, 24, 180, 592)
		})
	}
}

func BenchmarkBackgrounds(b *testing.B) {
	config := Defaults()
	config.Backgrounds = config.Backgrounds[:5]
	l := &Library{Config: config}
	c := ui.New(640, 240)
	for i := range config.Backgrounds {
		p := &config.Backgrounds[i]
		p.Speed = 1
		p.Density = 40
		p.color = 0x80bfff
		v := .7
		p.Intensity = &v
	}
	for i, p := range config.Backgrounds {
		b.Run(p.Name, func(b *testing.B) {
			var r Renderer
			f := Frame{Now: time.Now(), Levels: [2]float64{.2, .3}, Artwork: image.NewRGBA(image.Rect(0, 0, 128, 128))}
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				f.Now = f.Now.Add(time.Second / 60)
				clear(c.Pixels)
				r.Draw(c, l, i, f)
			}
		})
	}
}

func BenchmarkSpriteFormation(b *testing.B) {
	l, err := Load("../../music.json")
	if err != nil {
		b.Fatal(err)
	}
	index := l.Index("Toasty Squadron")
	if index < 0 {
		b.Skip("Toasty assets not installed")
	}
	c := ui.New(640, 240)
	var r Renderer
	f := Frame{Now: time.Now()}
	r.Draw(c, l, index, f)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		f.Now = f.Now.Add(time.Second / 60)
		clear(c.Pixels)
		r.Draw(c, l, index, f)
	}
}

func TestLazyAssetsPublishNewLibrary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "music.json")
	os.WriteFile(path, []byte(`{"default":"Custom","backgrounds":[{"name":"Custom","type":"image","files":["later.gif"]}]}`), 0600)
	l, err := LoadPresets(path)
	if err != nil {
		t.Fatal(err)
	}
	if l.Ready(0) {
		t.Fatal("missing asset marked ready")
	}
	if _, err = l.LoadAssets(0); err == nil {
		t.Fatal("missing asset accepted")
	}
	f, _ := os.Create(filepath.Join(dir, "later.gif"))
	gif.Encode(f, image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black}), nil)
	f.Close()
	loaded, err := l.LoadAssets(0)
	if err != nil {
		t.Fatal(err)
	}
	if l.Ready(0) || !loaded.Ready(0) || len(l.Config.Backgrounds[0].frames) != 0 {
		t.Fatal("asset loader mutated published library")
	}
}

func TestPartialMusicSettingsKeepAvailableDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	path := filepath.Join(dir, "music.json")
	for _, data := range []string{`{}`, `{"meters":false}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		library, err := LoadPresets(path)
		if err != nil {
			t.Fatal(err)
		}
		if library.Index(library.Config.Default) < 0 || len(library.Config.Backgrounds) < 6 {
			t.Fatal("lost built-in defaults")
		}
		if data != `{}` && library.Config.Meters {
			t.Fatal("explicit false ignored")
		}
	}
	for _, data := range []string{`null`, `[]`, `{"default":"Toasty","backgrounds":[{"name":"Toasty","type":"sprites"}]}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPresets(path); err == nil {
			t.Fatalf("accepted invalid settings or missing explicit assets: %s", data)
		}
	}
}

// TestDescriptiveVisualSettings verifies the documented names and legacy aliases
// preserve explicit false values without changing the selected background.
func TestDescriptiveVisualSettings(t *testing.T) {
	for _, data := range []string{
		`{"default_background":"Off","show_audio_meters":false}`,
		`{"default":"Off","meters":false}`,
		`{"default":"Starfield","meters":true,"default_background":"Off","show_audio_meters":false}`,
	} {
		library, err := ParsePresets(settings.Section{Path: filepath.Join(t.TempDir(), "settings.json"), Data: []byte(data)})
		if err != nil {
			t.Fatal(err)
		}
		if library.Config.Default != "Off" || library.Config.Meters {
			t.Fatalf("lost explicit visual settings: %+v", library.Config)
		}
	}
}

func TestMusicVisualFieldsRejectInvalidTypesAndHonorNull(t *testing.T) {
	for _, data := range []string{`{"show_audio_meters":"false"}`, `{"default_background":42}`} {
		if _, err := ParsePresets(settings.Section{Data: []byte(data)}); err == nil {
			t.Fatalf("accepted invalid visual setting %s", data)
		}
	}
	library, err := ParsePresets(settings.Section{Data: []byte(`{"default":"Off","meters":false,"default_background":null,"show_audio_meters":null}`)})
	if err != nil {
		t.Fatal(err)
	}
	if library.Config.Default != "Starfield" || !library.Config.Meters {
		t.Fatal("explicit null did not select defaults")
	}
}
