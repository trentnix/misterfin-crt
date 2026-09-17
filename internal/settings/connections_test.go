package settings

import "testing"

func TestNamedConnectionsValidation(t *testing.T) {
	valid := `{"profiles":[{"id":"home","name":"Home Jellyfin","server":{"provider":"jellyfin","url":"http://jellyfin:8096"}},{"id":"plex","name":"Plex","server":{"provider":"plex","url":"http://plex:32400"}}]}`
	profiles, err := ParseConnections(Section{Data: []byte(valid)})
	if err != nil || len(profiles) != 2 || profiles[0].Server.Transcode.MaxWidth != 720 || profiles[1].Server.Provider != "plex" {
		t.Fatalf("profiles: %+v %v", profiles, err)
	}
	for _, input := range []string{`null`, `[]`, `{"profiles":false}`, `{"profiles":[{"id":"../escape","name":"x","server":{"url":"http://server"}}]}`, `{"profiles":[{"id":"x","name":"x"}]}`, `{"profiles":[{"id":"x","name":"x","server":{"url":"http://server","password":"secret"}}]}`, `{"profiles":[{"id":"x","name":"x","server":{"url":"http://server"}},{"id":"x","name":"y","server":{"url":"http://other"}}]}`} {
		if _, err := ParseConnections(Section{Data: []byte(input)}); err == nil {
			t.Fatalf("accepted invalid profiles: %s", input)
		}
	}
	if profiles, err := ParseConnections(Section{}); err != nil || len(profiles) != 0 {
		t.Fatal("absent connections must preserve single-server behavior")
	}
}
