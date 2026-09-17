package plex

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"mistervision/internal/serverstate"
)

// ErrCodeExpired indicates an unapproved Plex link code reached its deadline.
var ErrCodeExpired = errors.New("Plex link code expired")

// ErrSessionSave indicates the private sign-in store could not be read or written.
var ErrSessionSave = errors.New("cannot read or save Plex session")

// StateDir isolates Plex credentials from the existing Jellyfin sign-in.
func StateDir(root string) string { return filepath.Join(root, "plex") }

// Authenticate validates a saved token or shows a short code for plex.tv/link.
// Only explicit rejection replaces a saved token. Network failures retain it.
// The caller must serialize authentication against other client operations.
func (c *Client) Authenticate(ctx context.Context, dir string, showCode func(string)) error {
	if c.Session.Token != "" {
		if err := c.validate(ctx); err == nil {
			return c.save(dir)
		} else if !Rejected(err) {
			return err
		}
	}
	if err := c.linkAccount(ctx, showCode); err != nil {
		return err
	}
	if err := c.validate(ctx); err != nil {
		return err
	}
	return c.save(dir)
}

// linkAccount obtains an account token without assuming a media server has been
// selected. The caller validates the account or server before saving credentials.
func (c *Client) linkAccount(ctx context.Context, showCode func(string)) error {
	var pin struct {
		ID        int    `json:"id"`
		Code      string `json:"code"`
		ExpiresIn int    `json:"expiresIn"`
		AuthToken string `json:"authToken"`
	}
	data, _, err := c.fetch(ctx, c.accountHTTP, c.accountURL, "", "POST", "/api/v2/pins", url.Values{"strong": {"false"}})
	if err != nil {
		return err
	}
	if json.Unmarshal(data, &pin) != nil || pin.ID <= 0 || pin.Code == "" || len(pin.Code) > 12 || pin.ExpiresIn <= 0 {
		return errors.New("invalid Plex link code response")
	}
	if showCode != nil {
		showCode(pin.Code)
	}
	deadline := time.NewTimer(time.Duration(min(pin.ExpiresIn, 900)) * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return ErrCodeExpired
		case <-poll.C:
			data, _, err = c.fetch(ctx, c.accountHTTP, c.accountURL, "", "GET", "/api/v2/pins/"+strconv.Itoa(pin.ID), url.Values{"code": {pin.Code}})
			if err != nil {
				return err
			}
			if json.Unmarshal(data, &pin) != nil {
				return errors.New("invalid Plex link response")
			}
			if pin.AuthToken == "" {
				continue
			}
			c.Session.Token = pin.AuthToken
			c.Session.UserID = ""
			return nil
		}
	}
}

func (c *Client) validate(ctx context.Context) error {
	// A library request requires authentication even on servers exposing identity.
	var result containerResponse
	if err := c.json(ctx, "/library/sections", nil, &result); err != nil {
		return err
	}
	if result.Container == nil {
		return errors.New("invalid Plex library response")
	}
	if c.Session.UserID != "" {
		return nil
	}
	user, err := c.accountUser(ctx)
	if err == nil {
		c.Session.UserID = strconv.Itoa(user.ID)
	}
	return err
}

// accountIdentity contains only the account fields needed for selection and
// cache isolation. Tokens and other private response fields are never retained.
type accountIdentity struct {
	ID           int    `json:"id"`
	Username     string `json:"username"`
	FriendlyName string `json:"friendlyName"`
}

// name supplies a bounded, single-line label even for unusual account names.
func (u accountIdentity) name() string {
	name := u.FriendlyName
	if strings.TrimSpace(name) == "" {
		name = u.Username
	}
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name))
	if name == "" {
		return "Plex account"
	}
	return string([]rune(name)[:min(64, len([]rune(name)))])
}

func (c *Client) accountUser(ctx context.Context) (accountIdentity, error) {
	data, _, err := c.fetch(ctx, c.accountHTTP, c.accountURL, c.Session.Token, "GET", "/api/v2/user", nil)
	var user accountIdentity
	if err != nil {
		return user, err
	}
	if json.Unmarshal(data, &user) != nil || user.ID <= 0 {
		return user, errors.New("invalid Plex account response")
	}
	return user, nil
}

func (c *Client) save(dir string) error {
	if err := serverstate.SaveSession(dir, c.Session); err != nil {
		return ErrSessionSave
	}
	return nil
}
