package main

import (
	"mistervision/internal/connection"
	jfconnection "mistervision/internal/jellyfin/connection"
	"mistervision/internal/plex"
	"mistervision/internal/serverstate"
	"mistervision/internal/settings"
	"os"
	"path/filepath"
	"testing"
)

func TestConnectionCatalogProfilesAndRememberedStartup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := `{"display":{"interlaced":true},"connections":{"profiles":[{"id":"jellyfin","name":"Jellyfin","server":{"url":"http://jellyfin:8096"}},{"id":"plex","name":"Plex","server":{"provider":"plex","url":"http://plex:32400"}}]}}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	load := func() *connectionCatalog {
		t.Helper()
		source, err := settings.Load(path, true)
		if err != nil {
			t.Fatal(err)
		}
		c, err := newConnectionCatalog(source, filepath.Join(dir, "jellyfin.conf"), dir, "test", nil)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := load()
	if c.selected != "profile/jellyfin" || len(c.choices) != 3 || c.choices[0].Name != "Use existing connection" || len(c.choices[0].Children) != 2 {
		t.Fatalf("catalog: %+v", c.choices)
	}
	jf := c.connectors["profile/jellyfin"].(*connection.Retained).Connector.(jfconnection.Connector)
	px := c.connectors["profile/plex"].(*connection.Retained).Connector.(plex.Connector)
	if jf.StateDir == px.StateDir || jf.StateDir == dir || px.StateDir == dir {
		t.Fatal("account storage is shared")
	}
	if err := c.connectors["profile/plex"].(*connection.Retained).Remember(); err != nil {
		t.Fatal(err)
	}
	if c = load(); c.selected != "profile/plex" {
		t.Fatal("startup did not restore successful choice")
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Fatal("choosing a connection edited settings")
	}
	if err := os.WriteFile(path, []byte(`{"server":{"url":"http://changed"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if c = load(); c.selected != "default" {
		t.Fatal("changed configuration did not reset startup choice")
	}
}

func TestPlexDiscoveryCatalogWithoutServerConfiguration(t *testing.T) {
	dir := t.TempDir()
	source, err := settings.Load(filepath.Join(dir, "settings.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	load := func() *connectionCatalog {
		t.Helper()
		catalog, err := newConnectionCatalog(source, filepath.Join(dir, "missing.conf"), dir, "test", nil)
		if err != nil {
			t.Fatal(err)
		}
		return catalog
	}
	catalog := load()
	if len(catalog.choices) != 2 || catalog.choices[1].ID != "plex-new" || catalog.choices[1].Help != "" {
		t.Fatal("Plex still requires configuration")
	}
	if catalog.connectionID("plex-new") != "plex" || catalog.connectionID("jellyfin-new") != "jellyfin" {
		t.Fatal("selection does not share remembered navigation")
	}
	first := catalog.connectors["plex-new"]
	catalog.startSelection("plex-new")
	if first == catalog.connectors["plex-new"] {
		t.Fatal("new selection reused old tentative connector")
	}
	px := catalog.discoveries["plex"].Connector.(plex.Connector)
	if px.Config.Server != "" {
		t.Fatal("discovery inherited explicit configuration")
	}
	server := connection.Server{ID: "server", Name: "My Plex", URL: "http://plex:32400"}
	if err := serverstate.SaveServer(filepath.Join(plex.StateDir(px.StateDir), "server.json"), server); err != nil {
		t.Fatal(err)
	}
	if err := catalog.discoveries["plex"].Remember(); err != nil {
		t.Fatal(err)
	}
	catalog = load()
	if catalog.selected != "plex" || catalog.choices[0].Children[0].ID != "plex" || catalog.choices[0].Children[0].Name != "My Plex" {
		t.Fatal("remembered Plex missing from startup or existing connections")
	}
}
