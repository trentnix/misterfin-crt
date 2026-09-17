package plex

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"mistervision/internal/connection"
)

var (
	errNoServers          = errors.New("Plex account has no media servers")
	errServersUnreachable = errors.New("no Plex server connection is reachable")
	errDiscovery          = errors.New("cannot discover Plex servers")
)

// serverDiscovery translates account resources into public server choices.
// Per-server grants remain private to this attempt and never enter UI state.
type serverDiscovery struct {
	account *Client
	lan     connection.Discoverer
	grants  map[connection.Server]string
}

var _ connection.Discoverer = (*serverDiscovery)(nil)

type accountResource struct {
	Name          string               `json:"name"`
	ID            string               `json:"clientIdentifier"`
	Provides      string               `json:"provides"`
	Token         string               `json:"accessToken"`
	HTTPSRequired bool                 `json:"httpsRequired"`
	Connections   []resourceConnection `json:"connections"`
}

type resourceConnection struct {
	URI   string `json:"uri"`
	Local bool   `json:"local"`
	Relay bool   `json:"relay"`
}

// Discover finds one verified direct endpoint per accessible media server.
// Local endpoints precede remote endpoints, with HTTPS first within each group.
// Probes are anonymous, bounded, and joined before returning on cancellation.
func (d *serverDiscovery) Discover(ctx context.Context) ([]connection.Server, error) {
	d.grants = make(map[connection.Server]string)
	// Scan while plex.tv responds. Always join the scan, including on account
	// failure or cancellation. A LAN failure cannot hide account endpoints.
	workLAN, cancelLAN := context.WithCancel(ctx)
	localDone := make(chan struct{})
	var local []connection.Server
	if d.lan == nil {
		close(localDone)
	} else {
		go func() {
			defer close(localDone)
			var err error
			local, err = d.lan.Discover(workLAN)
			d.account.Diagnostics.Record("connection.lan-discovery", slog.String("provider", "plex"), slog.Int("servers", len(local)), slog.Bool("failed", err != nil))
		}()
	}
	defer func() { cancelLAN(); <-localDone }()
	data, _, err := d.account.fetch(ctx, d.account.accountHTTP, d.account.accountURL, d.account.Session.Token, "GET", "/api/v2/resources", url.Values{"includeHttps": {"1"}, "includeRelay": {"0"}})
	if err != nil {
		return nil, err
	}
	var resources []accountResource
	if json.Unmarshal(data, &resources) != nil || len(resources) > 256 {
		return nil, errDiscovery
	}
	var servers []accountResource
	seen := make(map[string]bool)
	for _, resource := range resources {
		provides := strings.Split(resource.Provides, ",")
		isServer := false
		for _, capability := range provides {
			isServer = isServer || strings.TrimSpace(capability) == "server"
		}
		if !isServer || resource.Token == "" || resource.ID == "" || seen[resource.ID] {
			continue
		}
		seen[resource.ID] = true
		servers = append(servers, resource)
	}
	if len(servers) == 0 {
		return nil, errNoServers
	}
	<-localDone
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Account membership supplies authorization and the display name. GDM
	// supplies only another address, which still must pass /identity checks.
	for i := range servers {
		var nearby []resourceConnection
		for _, candidate := range local {
			if candidate.ID == servers[i].ID && candidate.Validate() == nil {
				nearby = append(nearby, resourceConnection{URI: candidate.URL, Local: true})
			}
		}
		servers[i].Connections = append(nearby, servers[i].Connections...)
	}
	work, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	results := make([]connection.Server, len(servers))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(4, len(servers)) {
		workers.Go(func() {
			for index := range jobs {
				results[index] = d.reachable(work, servers[index])
			}
		})
	}
	for index := range servers {
		select {
		case jobs <- index:
		case <-work.Done():
		}
		if work.Err() != nil {
			break
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var choices []connection.Server
	for index, candidate := range results {
		if candidate.ID == "" {
			continue
		}
		choices = append(choices, candidate)
		d.grants[candidate] = servers[index].Token
	}
	if len(choices) == 0 {
		return nil, errServersUnreachable
	}
	sort.Slice(choices, func(i, j int) bool {
		if choices[i].Name == choices[j].Name {
			return choices[i].ID < choices[j].ID
		}
		return strings.ToLower(choices[i].Name) < strings.ToLower(choices[j].Name)
	})
	return choices, nil
}

// reachable checks preference groups in order. Each group probes concurrently
// for at most two seconds, so unreachable container interfaces cannot consume
// the whole scan budget before a LAN or remote fallback gets a chance.
func (d *serverDiscovery) reachable(ctx context.Context, resource accountResource) connection.Server {
	var groups [4][]connection.Server
	seen := make(map[string]bool)
	for _, endpoint := range resource.Connections {
		candidate := connection.Server{ID: resource.ID, Name: resource.Name, URL: strings.TrimRight(endpoint.URI, "/")}
		if endpoint.Relay || seen[candidate.URL] || candidate.Validate() != nil {
			continue
		}
		seen[candidate.URL] = true
		if resource.HTTPSRequired && !strings.HasPrefix(candidate.URL, "https://") {
			continue
		}
		rank := endpoint.rank()
		// Bound sockets per server and retain room for every fallback group.
		if len(groups[rank]) < 16 {
			groups[rank] = append(groups[rank], candidate)
		}
	}
	for _, candidates := range groups {
		if ctx.Err() != nil {
			break
		}
		if server := d.probeGroup(ctx, candidates); server.ID != "" {
			return server
		}
	}
	return connection.Server{}
}

// probeGroup takes the first verified response among equally preferred
// endpoints. It cancels and drains the other probes before returning.
func (d *serverDiscovery) probeGroup(ctx context.Context, candidates []connection.Server) connection.Server {
	if len(candidates) == 0 {
		return connection.Server{}
	}
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	results := make(chan connection.Server, len(candidates))
	for _, candidate := range candidates {
		go func() {
			if d.account.verifyIdentity(probe, candidate) != nil {
				candidate = connection.Server{}
			}
			results <- candidate
		}()
	}
	var selected connection.Server
	for range candidates {
		if candidate := <-results; selected.ID == "" && candidate.ID != "" {
			selected = candidate
			cancel()
		}
	}
	return selected
}

func (c resourceConnection) rank() int {
	rank := 0
	if !c.Local {
		rank += 2
	}
	if !strings.HasPrefix(c.URI, "https://") {
		rank++
	}
	return rank
}

// verifyIdentity checks the public identity without sending an account or
// server token. A matching identity is required before using a discovered grant.
func (c *Client) verifyIdentity(ctx context.Context, server connection.Server) error {
	data, _, err := c.fetch(ctx, c.HTTP, server.URL, "", "GET", "/identity", nil)
	if err != nil {
		return err
	}
	var identity struct {
		Container struct {
			ID string `json:"machineIdentifier"`
		} `json:"MediaContainer"`
	}
	if json.Unmarshal(data, &identity) != nil || identity.Container.ID != server.ID {
		return errors.New("Plex server identity does not match")
	}
	return nil
}
