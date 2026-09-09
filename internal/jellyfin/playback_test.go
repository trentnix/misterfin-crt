package jellyfin

import (
	"net/url"
	"reflect"
	"testing"
)

func TestVideoStreamQueryMatchesC(t *testing.T) {
	c := NewClient(Config{Server: "http://example.test/jellyfin"}, Session{Token: "private & token", DeviceID: "device"})
	for _, ntsc := range []bool{false, true} {
		u, err := url.Parse(c.VideoStreamURL("movie", "session", 12345678, ntsc))
		if err != nil {
			t.Fatal(err)
		}
		fps := "25"
		if ntsc {
			fps = "30"
		}
		want := url.Values{"static": {"false"}, "videoCodec": {"mpeg2video"}, "container": {"ts"}, "audioCodec": {"mp3"}, "audioChannels": {"2"}, "allowVideoStreamCopy": {"false"}, "audioSampleRate": {"48000"}, "maxWidth": {"720"}, "maxHeight": {"576"}, "videoBitRate": {"12000000"}, "maxFramerate": {fps}, "startTimeTicks": {"12345678"}, "playSessionId": {"session"}, "deviceId": {"device"}, "ApiKey": {"private & token"}}
		if u.Path != "/jellyfin/Videos/movie/stream" || !reflect.DeepEqual(u.Query(), want) {
			t.Fatal("stream query differs from C")
		}
	}
	first, _ := NewPlaySessionID()
	second, _ := NewPlaySessionID()
	if first == "" || first == second {
		t.Fatal("play session IDs must be unique")
	}
}

func TestAudioStreamQueryMatchesC(t *testing.T) {
	c := NewClient(Config{Server: "https://server/jellyfin"}, Session{Token: "private & token"})
	u, err := url.Parse(c.AudioStreamURL("track", "session"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/jellyfin/Audio/track/stream" || !reflect.DeepEqual(u.Query(), url.Values{"static": {"true"}, "playSessionId": {"session"}, "ApiKey": {"private & token"}}) {
		t.Fatal("audio query differs from C")
	}
}
