package jellyfin

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
)

// Authentication failures identify recoverable sign-in conditions. Presentation
// and physical button instructions belong to the client UI, not this package.
var (
	// ErrQuickConnectDisabled means the server disallows code-based sign-in.
	ErrQuickConnectDisabled = errors.New("Quick Connect is disabled")
	// ErrQuickConnectExpired means the approval deadline elapsed.
	ErrQuickConnectExpired = errors.New("Quick Connect expired")
	// ErrUsernameNotFound means no server account matched the configured name.
	ErrUsernameNotFound = errors.New("configured username was not found")
)

// Authenticate preserves saved tokens on temporary failures. Only an explicit
// 401/403 starts replacement sign-in. When no complete saved session exists,
// a configured API key takes precedence over Quick Connect. A rejected saved
// session goes directly to Quick Connect, matching the existing client behavior.
//
// Authenticate mutates Session and must not run concurrently with other Client
// operations. showCode must be non-nil if Quick Connect is needed. It runs
// synchronously and receives only the public approval code. The caller must
// persist Session after successful authentication. The returned User comes from
// the authenticated response, so callers need no separate identity request.
// Cancellation stops polling and returns the context error.
func (c *Client) Authenticate(ctx context.Context, showCode func(string)) (User, error) {
	if c.Session.Token != "" && c.Session.UserID != "" {
		if c.Config.APIKey != "" && c.Session.Token == c.Config.APIKey {
			return c.authenticateAPIKey(ctx)
		}
		user, err := c.CurrentUser(ctx)
		if err == nil {
			return user, nil
		}
		if !Rejected(err) {
			return User{}, err
		}
		c.Session.Token = ""
		c.Session.UserID = ""
	} else if c.Config.APIKey != "" {
		return c.authenticateAPIKey(ctx)
	}
	return c.authenticateQuickConnect(ctx, showCode)
}

// authenticateAPIKey resolves the configured username without creating a saved
// Quick Connect token. It leaves the configured API key on Session.
func (c *Client) authenticateAPIKey(ctx context.Context) (User, error) {
	c.Session.Token = c.Config.APIKey
	var users []User
	if err := c.json(ctx, "GET", "/Users", nil, nil, &users); err != nil {
		return User{}, err
	}
	for _, u := range users {
		if u.ID != "" && strings.EqualFold(u.Name, c.Config.Username) {
			c.Session.UserID = u.ID
			return u, nil
		}
	}
	return User{}, ErrUsernameNotFound
}

// authenticateQuickConnect publishes the user code, polls until approved or
// canceled, and installs the resulting token on Session. The secret stays inside requests.
func (c *Client) authenticateQuickConnect(ctx context.Context, showCode func(string)) (User, error) {
	c.Session.Token = ""
	var enabled bool
	if err := c.json(ctx, "GET", "/QuickConnect/Enabled", nil, nil, &enabled); err != nil {
		return User{}, err
	}
	if !enabled {
		return User{}, ErrQuickConnectDisabled
	}
	var qc struct {
		Secret, Code  string
		Authenticated bool
	}
	if err := c.json(ctx, "POST", "/QuickConnect/Initiate", nil, nil, &qc); err != nil {
		return User{}, err
	}
	if qc.Secret == "" || qc.Code == "" {
		return User{}, errors.New("invalid Quick Connect response")
	}
	showCode(qc.Code)
	deadline := time.NewTimer(5 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return User{}, ctx.Err()
		case <-deadline.C:
			return User{}, ErrQuickConnectExpired
		case <-ticker.C:
		}
		var result struct{ Authenticated bool }
		if err := c.json(ctx, "GET", "/QuickConnect/Connect", url.Values{"secret": {qc.Secret}}, nil, &result); err != nil {
			return User{}, err
		}
		if !result.Authenticated {
			continue
		}
		var login struct {
			AccessToken string
			User        User
		}
		if err := c.json(ctx, "POST", "/Users/AuthenticateWithQuickConnect", nil, map[string]string{"Secret": qc.Secret}, &login); err != nil {
			return User{}, err
		}
		if login.AccessToken == "" || login.User.ID == "" || login.User.Name == "" {
			return User{}, errors.New("invalid sign-in response")
		}
		c.Session.Token = login.AccessToken
		c.Session.UserID = login.User.ID
		return login.User, nil
	}
}
