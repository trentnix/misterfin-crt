package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mistervision/internal/connection"
	jfconnection "mistervision/internal/jellyfin/connection"
	"mistervision/internal/media"
	"mistervision/internal/plex"
	"mistervision/internal/serverstate"
	"mistervision/internal/settings"
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
	jf := c.connectors["profile/jellyfin"].(*connection.Retained).Connector.(*jfconnection.Connector)
	px := c.connectors["profile/plex"].(*connection.Retained).Connector.(plex.Connector)
	if jf.StateDir == px.StateDir || jf.StateDir == dir || px.StateDir == dir {
		t.Fatal("account storage is shared")
	}
	if err := c.connectors["profile/plex"].(*connection.Retained).Remember(connection.Server{}); err != nil {
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
	if len(catalog.choices) != 2 || catalog.choices[0].ID != "jellyfin-new" || catalog.choices[1].ID != "plex-new" || catalog.choices[1].Help != "" {
		t.Fatal("fresh setup must offer both providers without an existing connection")
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
	if err := catalog.discoveries["plex"].Remember(server); err != nil {
		t.Fatal(err)
	}
	catalog = load()
	if catalog.selected != "plex" || catalog.choices[0].Children[0].ID != "plex" || catalog.choices[0].Children[0].Name != "My Plex" {
		t.Fatal("remembered Plex missing from startup or existing connections")
	}
}

// profileCatalogConnector supplies distinct viewers without network or storage.
type profileCatalogConnector struct {
	fail   bool
	calls  int
	action connection.ProfileAction
}
type catalogViewer struct {
	media.Server
	user string
}

func (v catalogViewer) Identity() media.Identity { return media.Identity{Server: "plex", User: v.user} }
func (c *profileCatalogConnector) Connect(ctx context.Context, i connection.Interaction) (connection.Session, error) {
	c.calls++
	c.action = i.ProfileAction
	if c.fail {
		return connection.Session{}, context.Canceled
	}
	user := "first"
	if i.ProfileAction != connection.ProfileUnchanged {
		user = "second"
	}
	return connection.Session{Server: catalogViewer{user: user}, Profile: &connection.Profile{ID: user, Name: user}, ProfileAction: connection.ProfileChoose}, nil
}
func (*profileCatalogConnector) Describe(error) connection.Presentation {
	return connection.Presentation{}
}

func TestProfileCatalogCancelsAndPromotesTentativeViewer(t *testing.T) {
	provider := &profileCatalogConnector{}
	retained := &connection.Retained{Connector: provider}
	catalog := &connectionCatalog{connectors: map[string]connection.Connector{"profile/plex": retained}, retained: map[string]*connection.Retained{"profile/plex": retained}}
	original, err := retained.Connect(t.Context(), connection.Interaction{})
	if err != nil {
		t.Fatal(err)
	}
	route := catalog.startProfileSelection("profile/plex", connection.ProfileChoose)
	if catalog.connectionID(route) != "profile/plex" {
		t.Fatal("profile switch changed connection identity")
	}
	provider.fail = true
	if _, err := catalog.connectors[route].Connect(t.Context(), connection.Interaction{}); !errors.Is(err, context.Canceled) {
		t.Fatal("failed switch succeeded")
	}
	restored, err := retained.Connect(t.Context(), connection.Interaction{})
	if err != nil || restored.Server.Identity() != original.Server.Identity() {
		t.Fatal("cancel lost the working viewer")
	}
	provider.fail = false
	route = catalog.startProfileSelection("profile/plex", connection.ProfileChoose)
	replacement, err := catalog.connectors[route].Connect(t.Context(), connection.Interaction{})
	if err != nil || replacement.Profile.ID != "second" {
		t.Fatal("profile switch was not requested")
	}
	calls := provider.calls
	again, err := retained.Connect(t.Context(), connection.Interaction{})
	if err != nil || again.Server.Identity() != replacement.Server.Identity() || provider.calls != calls {
		t.Fatal("successful switch did not promote retained viewer")
	}
	if _, err := catalog.connectors[route].Connect(t.Context(), connection.Interaction{}); err != nil || provider.calls != calls {
		t.Fatal("promoted route reopened the picker")
	}
}

func TestProfileConnectionRetainsActionAndCancelRouteOnFailure(t *testing.T) {
	for _, action := range []connection.ProfileAction{connection.ProfileChoose, connection.ProfileAdd} {
		provider := &profileCatalogConnector{fail: true}
		attempt := profileConnection{connector: provider, action: action}
		for range 2 {
			back := connection.BackDefault
			_, err := attempt.Connect(t.Context(), connection.Interaction{Progress: func(p connection.Presentation) { back = p.Back }})
			if !errors.Is(err, context.Canceled) || provider.action != action || back != connection.BackConnection {
				t.Fatal("failed attempt lost its action or route back to the connected browser")
			}
		}
		_, _ = attempt.Connect(t.Context(), connection.Interaction{ProfileAction: connection.ProfileChoose})
		if provider.action != connection.ProfileChoose {
			t.Fatal("Back could not reopen the picker during direct Add user")
		}
	}
}

func TestConnectionCatalogKeepsNewChoicesAndUpdatesMetadata(t *testing.T) {
	dir := t.TempDir()
	source, err := settings.Load(filepath.Join(dir, "settings.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newConnectionCatalog(source, filepath.Join(dir, "missing.conf"), dir, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	initial := catalog.Choices()
	// No provider state files exist. Published metadata must be sufficient.
	for _, id := range []string{"jellyfin", "plex"} {
		for _, name := range []string{"Original " + id, "Renamed " + id} {
			server := connection.Server{ID: id, Name: name, URL: "http://" + id}
			snapshot := catalog.Choices()
			if err := catalog.discoveries[id].Remember(server); err != nil {
				t.Fatal(err)
			}
			choices := catalog.Choices()
			if choices[0].ID != "existing" {
				t.Fatal("successful discovery did not create existing connections")
			}
			found := false
			for _, child := range choices[0].Children {
				if child.ID == id && child.Name == name {
					found = true
				}
			}
			if !found {
				t.Fatal("successful selection did not refresh connection metadata")
			}
			if snapshot[0].ID == "existing" {
				for _, child := range snapshot[0].Children {
					if child.ID == id && child.Name == name {
						t.Fatal("publication mutated an earlier snapshot")
					}
				}
			}
		}
	}
	if len(initial) != 2 || initial[0].ID != "jellyfin-new" {
		t.Fatal("initial menu was mutated")
	}
	choices := catalog.Choices()[0].Children
	if len(choices) != 2 || choices[0].ID != "jellyfin" || choices[1].ID != "plex" {
		t.Fatal("switching connections lost an earlier discovery")
	}
	// The default startup route can discover Jellyfin before either provider
	// route is chosen. It must publish that connection through the same owner.
	server := connection.Server{ID: "default", Name: "Startup server", URL: "http://startup"}
	if err := catalog.retained["default"].Remember(server); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Choices()[0].Children) != 3 {
		t.Fatal("default discovery missing from catalog")
	}
}
