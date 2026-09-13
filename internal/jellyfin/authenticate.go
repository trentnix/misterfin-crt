package jellyfin

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
)

// Authenticate preserves saved tokens on temporary failures. Only an explicit
// 401/403 starts replacement sign-in. When no complete saved session exists,
// a configured API key takes precedence over Quick Connect. A rejected saved
// session goes directly to Quick Connect, matching the existing client behavior.
//
// Authenticate mutates Session and must not run concurrently with other Client
// operations. showCode must be non-nil if Quick Connect is needed. It runs
// synchronously and receives only the public approval code. Quick Connect tokens
// are saved under dir. Cancellation stops polling and returns the context error.
func (c *Client) Authenticate(ctx context.Context, dir string, showCode func(string)) error {
	if c.Session.Token != "" && c.Session.UserID != "" {
		_, err := c.List(ctx, Location{Kind: "views"}, 0, 1)
		if err == nil {
			return nil
		}
		if !Rejected(err) {
			return err
		}
		c.Session.Token = ""
		c.Session.UserID = ""
	} else if c.Config.APIKey != "" {
		return c.authenticateAPIKey(ctx)
	}
	return c.authenticateQuickConnect(ctx, dir, showCode)
}

// authenticateAPIKey resolves the configured username without creating a saved
// Quick Connect token. It leaves the configured API key on Session.
func (c *Client) authenticateAPIKey(ctx context.Context) error {
	c.Session.Token = c.Config.APIKey
	var users []Item
	if err := c.json(ctx, "GET", "/Users", nil, nil, &users); err != nil {
		return err
	}
	for _, u := range users {
		if strings.EqualFold(u.Name, c.Config.Username) {
			c.Session.UserID = u.ID
			break
		}
	}
	if c.Session.UserID == "" {
		return errors.New("configured username was not found")
	}
	return nil
}

// authenticateQuickConnect publishes the user code, polls until approved or
// canceled, and persists the resulting token. The secret stays inside requests.
func (c *Client) authenticateQuickConnect(ctx context.Context, dir string, showCode func(string)) error {
	c.Session.Token = ""
	var enabled bool
	if err := c.json(ctx, "GET", "/QuickConnect/Enabled", nil, nil, &enabled); err != nil {
		return err
	}
	if !enabled {
		return errors.New("Quick Connect is disabled; configure an API key and username")
	}
	var qc struct {
		Secret, Code  string
		Authenticated bool
	}
	if err := c.json(ctx, "POST", "/QuickConnect/Initiate", nil, nil, &qc); err != nil {
		return err
	}
	if qc.Secret == "" || qc.Code == "" {
		return errors.New("invalid Quick Connect response")
	}
	showCode(qc.Code)
	deadline := time.NewTimer(5 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Quick Connect expired; press R to retry")
		case <-ticker.C:
		}
		var result struct{ Authenticated bool }
		if err := c.json(ctx, "GET", "/QuickConnect/Connect", url.Values{"secret": {qc.Secret}}, nil, &result); err != nil {
			return err
		}
		if !result.Authenticated {
			continue
		}
		var login struct {
			AccessToken string
			User        Item
		}
		if err := c.json(ctx, "POST", "/Users/AuthenticateWithQuickConnect", nil, map[string]string{"Secret": qc.Secret}, &login); err != nil {
			return err
		}
		if login.AccessToken == "" || login.User.ID == "" {
			return errors.New("invalid sign-in response")
		}
		c.Session.Token = login.AccessToken
		c.Session.UserID = login.User.ID
		if err := SaveSession(dir, c.Session); err != nil {
			return errors.New("signed in but cannot save Go session")
		}
		return nil
	}
}
