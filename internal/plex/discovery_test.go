package plex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mistervision/internal/connection"
	"mistervision/internal/serverstate"
)

// discoveryAccount serves the account API independently of candidate servers.
func discoveryAccount(t *testing.T, resources []accountResource) *Client {
	t.Helper()
	account := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/resources" || r.Header.Get("X-Plex-Token") != "account-secret" {
			t.Error("wrong account request")
			w.WriteHeader(401)
			return
		}
		if r.URL.Query().Get("includeHttps") != "1" || r.URL.Query().Get("includeRelay") != "0" {
			t.Error("wrong discovery policy")
		}
		json.NewEncoder(w).Encode(resources)
	}))
	t.Cleanup(account.Close)
	client := NewClient(Config{}, serverstate.Session{DeviceID: "device", Token: "account-secret"})
	client.accountURL = account.URL
	return client
}

func TestDiscoveryPrefersVerifiedLocalServerAndKeepsGrantsPrivate(t *testing.T) {
	var wrong, local, remote atomic.Int32
	probe := func(count *atomic.Int32, id string) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count.Add(1)
			if r.URL.Path != "/identity" || r.Header.Get("X-Plex-Token") != "" {
				t.Error("identity probe leaked credentials or requested media")
			}
			fmt.Fprintf(w, `{"MediaContainer":{"machineIdentifier":%q}}`, id)
		}))
		t.Cleanup(server.Close)
		return server
	}
	bad, good, far := probe(&wrong, "other"), probe(&local, "server"), probe(&remote, "server")
	resources := []accountResource{
		{Name: "A player", ID: "player", Provides: "client", Token: "private-player", Connections: []resourceConnection{{URI: far.URL}}},
		{Name: "Home", ID: "server", Provides: "server", Token: "server-secret", Connections: []resourceConnection{{URI: far.URL}, {URI: bad.URL, Local: true}, {URI: good.URL, Local: true}}},
		{Name: "Duplicate", ID: "server", Provides: "server", Token: "other-secret", Connections: []resourceConnection{{URI: far.URL}}},
	}
	d := &serverDiscovery{account: discoveryAccount(t, resources)}
	choices, err := d.Discover(t.Context())
	if err != nil || len(choices) != 1 {
		t.Fatalf("choices=%v error=%v", choices, err)
	}
	if choices[0].URL != good.URL || choices[0].Name != "Home" || d.grants[choices[0]] != "server-secret" {
		t.Fatal("wrong endpoint or grant")
	}
	if wrong.Load() > 1 || local.Load() != 1 || remote.Load() != 0 {
		t.Fatal("local endpoints did not precede remote endpoints")
	}
	if strings.Contains(fmt.Sprint(choices), "secret") {
		t.Fatal("public choices contain credentials")
	}
}

func TestDiscoveryRequiresValidIdentityAndTLS(t *testing.T) {
	for _, test := range []string{"wrong identity", "invalid JSON", "TLS required", "invalid certificate", "relay", "URL query", "URL credentials"} {
		t.Run(test, func(t *testing.T) {
			var calls atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"different"}}`)
			})
			if test == "invalid JSON" {
				handler = func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{"private":"response"}`) }
			}
			server := httptest.NewUnstartedServer(handler)
			if test == "invalid certificate" {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			uri := server.URL
			if test == "URL query" {
				uri += "?token=private"
			}
			if test == "URL credentials" {
				uri = strings.Replace(uri, "http://", "http://user:private@", 1)
			}
			resource := accountResource{Name: "Home", ID: "server", Provides: "server", Token: "server-secret", HTTPSRequired: test == "TLS required", Connections: []resourceConnection{{URI: uri, Local: true, Relay: test == "relay"}}}
			d := &serverDiscovery{account: discoveryAccount(t, []accountResource{resource})}
			choices, err := d.Discover(t.Context())
			if !errors.Is(err, errServersUnreachable) || len(choices) != 0 || len(d.grants) != 0 {
				t.Fatalf("unsafe server accepted: %v %v", choices, err)
			}
			if strings.Contains(fmt.Sprint(err), "private") {
				t.Fatal("error exposed response or URL")
			}
			if (test == "TLS required" || test == "relay" || strings.HasPrefix(test, "URL")) && calls.Load() != 0 {
				t.Fatal("invalid endpoint was contacted")
			}
		})
	}
}

func TestDiscoveryCancellationJoinsProbes(t *testing.T) {
	started, finished := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(finished) }))
	defer server.Close()
	d := &serverDiscovery{account: discoveryAccount(t, []accountResource{{Name: "Home", ID: "server", Provides: "server", Token: "server-secret", Connections: []resourceConnection{{URI: server.URL, Local: true}}}})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := d.Discover(ctx); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("discovery ignored cancellation")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("probe remained active")
	}
}

func TestDiscoveryHasNoServersAndRefusesRedirects(t *testing.T) {
	d := &serverDiscovery{account: discoveryAccount(t, nil)}
	if _, err := d.Discover(t.Context()); !errors.Is(err, errNoServers) {
		t.Fatal(err)
	}
	var contacted atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted.Store(true) }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/identity", http.StatusFound)
	}))
	defer redirect.Close()
	d.account = discoveryAccount(t, []accountResource{{Name: "Home", ID: "server", Provides: "server", Token: "server-secret", Connections: []resourceConnection{{URI: redirect.URL}}}})
	if _, err := d.Discover(t.Context()); !errors.Is(err, errServersUnreachable) || contacted.Load() {
		t.Fatal("cross-origin redirect followed")
	}
}

// localDiscoveryFunc supplies LAN candidates without broadcasting during
// account-discovery tests. Wire-level behavior is covered in gdm_test.go.
type localDiscoveryFunc func(context.Context) ([]connection.Server, error)

func (f localDiscoveryFunc) Discover(ctx context.Context) ([]connection.Server, error) { return f(ctx) }

func TestDiscoveryCombinesLANWithAccountAuthorization(t *testing.T) {
	for _, scenario := range []string{"local preferred", "unlisted server", "wrong identity", "LAN unavailable", "no LAN replies", "HTTPS required"} {
		t.Run(scenario, func(t *testing.T) {
			var localCalls, remoteCalls atomic.Int32
			local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				localCalls.Add(1)
				if r.Header.Get("X-Plex-Token") != "" {
					t.Error("LAN probe received a token")
				}
				id := "home"
				if scenario == "wrong identity" {
					id = "impostor"
				}
				fmt.Fprintf(w, `{"MediaContainer":{"machineIdentifier":%q}}`, id)
			}))
			defer local.Close()
			remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				remoteCalls.Add(1)
				if r.Header.Get("X-Plex-Token") != "" {
					t.Error("remote probe received a token")
				}
				fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"home"}}`)
			}))
			defer remote.Close()
			account := discoveryAccount(t, []accountResource{{Name: "Account server name", ID: "home", Provides: "server", Token: "server-grant", HTTPSRequired: scenario == "HTTPS required", Connections: []resourceConnection{{URI: remote.URL}}}})
			account.HTTP = remote.Client()
			d := &serverDiscovery{account: account, lan: localDiscoveryFunc(func(context.Context) ([]connection.Server, error) {
				if scenario == "LAN unavailable" {
					return nil, errors.New("UDP unavailable")
				}
				if scenario == "no LAN replies" {
					return nil, nil
				}
				id := "home"
				if scenario == "unlisted server" {
					id = "unlisted"
				}
				return []connection.Server{{ID: id, Name: "Untrusted LAN name", URL: local.URL}}, nil
			})}
			choices, err := d.Discover(t.Context())
			if err != nil || len(choices) != 1 {
				t.Fatalf("choices=%v err=%v", choices, err)
			}
			want := remote.URL
			if scenario == "local preferred" {
				want = local.URL
			}
			if choices[0].URL != want || choices[0].Name != "Account server name" || d.grants[choices[0]] != "server-grant" {
				t.Fatal("wrong server, name, or grant")
			}
			if scenario == "local preferred" && remoteCalls.Load() != 0 {
				t.Fatal("remote endpoint used ahead of LAN")
			}
			if (scenario == "unlisted server" || scenario == "HTTPS required") && localCalls.Load() != 0 {
				t.Fatal("ineligible LAN endpoint contacted")
			}
		})
	}
}

func TestAccountFailureCancelsAndJoinsLANDiscovery(t *testing.T) {
	account := discoveryAccount(t, nil)
	finished := make(chan struct{})
	d := &serverDiscovery{account: account, lan: localDiscoveryFunc(func(ctx context.Context) ([]connection.Server, error) {
		defer close(finished)
		<-ctx.Done()
		return nil, ctx.Err()
	})}
	done := make(chan error, 1)
	go func() { _, err := d.Discover(t.Context()); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, errNoServers) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("account failure did not cancel LAN scan")
	}
	select {
	case <-finished:
	default:
		t.Fatal("LAN scan outlived discovery")
	}
}

func TestDiscoveryCancellationJoinsLAN(t *testing.T) {
	account := discoveryAccount(t, []accountResource{{ID: "home", Name: "Home", Provides: "server", Token: "grant"}})
	started, finished := make(chan struct{}), make(chan struct{})
	d := &serverDiscovery{account: account, lan: localDiscoveryFunc(func(ctx context.Context) ([]connection.Server, error) {
		close(started)
		defer close(finished)
		<-ctx.Done()
		return nil, ctx.Err()
	})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := d.Discover(ctx); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("discovery did not cancel")
	}
	select {
	case <-finished:
	default:
		t.Fatal("LAN scan outlived cancellation")
	}
}

// discoveryTransportFunc simulates unreachable container interfaces without
// depending on local routes, DNS failures, or external servers.
type discoveryTransportFunc func(*http.Request) (*http.Response, error)

func (f discoveryTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDiscoveryContainerAddressesCannotStarveLANOrRemote(t *testing.T) {
	for _, target := range []string{"account HTTPS", "GDM HTTP", "remote HTTPS"} {
		t.Run(target, func(t *testing.T) {
			var blocked, active atomic.Int32
			resource := accountResource{ID: "home", Name: "Home", Provides: "server", Token: "grant"}
			for i := range 9 {
				resource.Connections = append(resource.Connections, resourceConnection{URI: fmt.Sprintf("https://container-%d.invalid", i), Local: true})
			}
			want := "https://reachable.invalid"
			if target == "GDM HTTP" {
				want = "http://reachable.invalid"
			} else {
				resource.Connections = append(resource.Connections, resourceConnection{URI: want, Local: target == "account HTTPS"})
			}
			account := discoveryAccount(t, []accountResource{resource})
			account.HTTP = &http.Client{Transport: discoveryTransportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("X-Plex-Token") != "" {
					t.Error("probe exposed credentials")
				}
				if r.URL.Host == "reachable.invalid" {
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"MediaContainer":{"machineIdentifier":"home"}}`))}, nil
				}
				blocked.Add(1)
				active.Add(1)
				defer active.Add(-1)
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}
			d := &serverDiscovery{account: account}
			if target == "GDM HTTP" {
				d.lan = localDiscoveryFunc(func(context.Context) ([]connection.Server, error) {
					return []connection.Server{{ID: "home", Name: "LAN", URL: want}}, nil
				})
			}
			ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
			defer cancel()
			choices, err := d.Discover(ctx)
			if err != nil || len(choices) != 1 || choices[0].URL != want {
				t.Fatalf("choices=%v err=%v", choices, err)
			}
			if active.Load() != 0 {
				t.Fatal("probe outlived discovery")
			}
			if target != "account HTTPS" && blocked.Load() != 9 {
				t.Fatalf("container probes=%d", blocked.Load())
			}
		})
	}
}
