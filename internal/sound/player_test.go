package sound

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mistervision/assets/sfx"
)

func TestConfigAndSilentDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sounds.json")
	cfg, err := LoadConfig(path)
	if err != nil || cfg != Defaults() {
		t.Fatalf("defaults: %+v %v", cfg, err)
	}
	for _, tc := range []struct {
		json  string
		valid bool
	}{
		{`{"enabled":false}`, true}, {`{"volume":0}`, true}, {`{"volume":100}`, true},
		{`{"volume":-1}`, false}, {`{"volume":101}`, false}, {`{"volume":0.5}`, false},
		{`{"volum":20}`, false}, {`{} {}`, false}, {`null`, false}, {`[]`, false},
	} {
		if err := os.WriteFile(path, []byte(tc.json), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadConfig(path)
		if (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.json, err)
		}
	}
	for _, cfg := range []Config{{Enabled: false, Volume: 20}, {Enabled: true, Volume: 0}} {
		p, err := New(cfg, func() (Stream, error) { t.Error("disabled audio opened"); return nil, nil })
		if err != nil || p != nil {
			t.Fatalf("disabled player: %v %v", p, err)
		}
		p.Play(Navigate)
		p.Suspend()()
		p.Close()
	}
}

func TestEmbeddedClipsAreQuietAndPreserveWaveform(t *testing.T) {
	for _, data := range [][]byte{sfx.Navigation, sfx.Confirmation} {
		full, err := decodeClip(data, 100)
		if err != nil {
			t.Fatal(err)
		}
		quiet, err := decodeClip(data, Defaults().Volume)
		if err != nil || len(full) != len(quiet) {
			t.Fatal("clip length changed")
		}
		audible := false
		for i, v := range full {
			if quiet[i] != int16(int(v)*10/100) {
				t.Fatal("waveform or gain changed")
			}
			audible = audible || quiet[i] != 0
		}
		if !audible {
			t.Fatal("clip is silent")
		}
		if _, err := decodeClip(data[:len(data)/2], 20); err == nil {
			t.Fatal("truncated clip accepted")
		}
	}
}

func TestPlaybackSuspensionReleasesAudioAndCountsReplacements(t *testing.T) {
	opened := make(chan *testStream, 4)
	p, err := New(Defaults(), func() (Stream, error) { s := newTestStream(); opened <- s; return s, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.Play(Navigate)
	s := await(t, opened)
	await(t, s.wrote)
	first := p.Suspend()
	select {
	case <-s.closed:
	default:
		t.Fatal("Suspend returned before audio closed")
	}
	second := p.Suspend()
	first()
	first()
	for i := 0; i < 10000; i++ {
		p.Play(Navigate)
	}
	select {
	case <-opened:
		t.Fatal("sound reopened during a replacement")
	case <-time.After(30 * time.Millisecond):
	}
	second()
	select {
	case <-opened:
		t.Fatal("stale sound replayed on release")
	case <-time.After(30 * time.Millisecond):
	}
	p.Play(Confirm)
	resumed := await(t, opened)
	await(t, resumed.wrote)
	p.Close()
	select {
	case <-resumed.closed:
	default:
		t.Fatal("Close retained audio")
	}
}

// A slow device must never stall input. Suspension must wait for that device
// to close, and clicks received during the handoff must not replay afterward.
func TestBusyDeviceDoesNotBlockNavigationOrLeakCues(t *testing.T) {
	opening := make(chan *testStream, 2)
	proceed := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(proceed) })
	p, err := New(Defaults(), func() (Stream, error) {
		stream := newTestStream()
		opening <- stream
		<-proceed
		return stream, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { unblock(); p.Close() }()
	p.Play(Navigate)
	stream := await(t, opening)

	pressed := make(chan bool, 1)
	go func() {
		for i := 0; i < 10000; i++ {
			p.Play(Confirm)
		}
		pressed <- true
	}()
	await(t, pressed) // The audio device is still blocked here.

	suspended := make(chan func(), 1)
	go func() { suspended <- p.Suspend() }()
	unblock()
	resume := await(t, suspended)
	select {
	case <-stream.closed:
	default:
		t.Fatal("Suspend returned before the opening device closed")
	}
	resume()
	select {
	case <-opening:
		t.Fatal("busy navigation replayed after playback suspension")
	case <-time.After(30 * time.Millisecond):
	}
}

func TestUnavailableDeviceDoesNotRetryForEveryPress(t *testing.T) {
	var opens atomic.Int32
	attempted := make(chan bool, 10)
	p, err := New(Defaults(), func() (Stream, error) { opens.Add(1); attempted <- true; return nil, errors.New("busy") })
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.Play(Navigate)
	await(t, attempted)
	for i := 0; i < 10000; i++ {
		p.Play(Navigate)
	}
	time.Sleep(30 * time.Millisecond)
	if opens.Load() != 1 {
		t.Fatalf("opened %d times", opens.Load())
	}
}

func TestIdleBurstReleasesAudio(t *testing.T) {
	s := newTestStream()
	p, err := New(Defaults(), func() (Stream, error) { return s, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.Play(Navigate)
	await(t, s.wrote)
	await(t, s.closed)
}

// testStream simulates immediate audio capacity without opening host devices.
type testStream struct{ wrote, closed chan bool }

func newTestStream() *testStream { return &testStream{make(chan bool, 1), make(chan bool)} }
func (s *testStream) Write(pcm []int16) (int, error) {
	select {
	case s.wrote <- true:
	default:
	}
	return len(pcm) / 2, nil
}
func (s *testStream) Close() error { close(s.closed); return nil }

// await bounds worker checks so a broken handoff fails without hanging tests.
func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sound worker")
	}
	var zero T
	return zero
}
