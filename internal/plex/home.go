package plex

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"mistervision/internal/connection"
)

var errHome = errors.New("cannot open Plex Home profile")

// homeUser decodes Plex's XML-only Home API. Account credentials and private
// response fields stay in this adapter, separate from public profile prompts.
type homeUser struct {
	ID        string `xml:"id,attr"`
	Title     string `xml:"title,attr"`
	Protected string `xml:"protected,attr"`
	Thumb     string `xml:"thumb,attr"`
}

// homeProfiles validates membership without waiting for avatar downloads.
func (c *Client) homeProfiles(ctx context.Context) ([]connection.Profile, connection.ProfileAvatars, error) {
	data, _, err := c.fetch(ctx, c.accountHTTP, c.accountURL, c.Session.Token, "GET", "/api/home/users", nil)
	if err != nil {
		return nil, nil, err
	}
	var response struct {
		Users []homeUser `xml:"User"`
	}
	if len(data) > 64<<10 || xml.Unmarshal(data, &response) != nil || len(response.Users) == 0 || len(response.Users) > 32 {
		return nil, nil, errHome
	}
	profiles := make([]connection.Profile, len(response.Users))
	seen := make(map[string]bool)
	for i, user := range response.Users {
		id, e := strconv.Atoi(user.ID)
		if e != nil || id <= 0 || seen[user.ID] || (user.Protected != "0" && user.Protected != "1" && user.Protected != "false" && user.Protected != "true") {
			return nil, nil, errHome
		}
		seen[user.ID] = true
		profiles[i] = connection.Profile{ID: user.ID, Name: (accountIdentity{FriendlyName: user.Title}).name(), Protected: user.Protected == "1" || user.Protected == "true"}
	}
	urls := make(map[string]string, len(response.Users))
	for _, user := range response.Users {
		urls[user.ID] = user.Thumb
	}
	return profiles, &homeAvatars{client: c.accountHTTP, urls: urls}, nil
}

// switchHome sends the PIN in a form body over the account transport. Neither
// HTTP diagnostics nor error strings include the PIN, token, or response body.
func (c *Client) switchHome(ctx context.Context, profile connection.Profile, pin string) (string, error) {
	if profile.Protected && (len(pin) != 4 || strings.IndexFunc(pin, func(r rune) bool { return r < '0' || r > '9' }) >= 0) {
		return "", &HTTPError{Status: http.StatusForbidden}
	}
	form := url.Values{}
	if pin != "" {
		form.Set("pin", pin)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.accountURL+"/api/home/users/"+url.PathEscape(profile.ID)+"/switch", strings.NewReader(form.Encode()))
	if err != nil {
		return "", errHome
	}
	c.headers(req, c.Session.Token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r, err := c.accountHTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errHome
	}
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return "", &HTTPError{Status: r.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, (64<<10)+1))
	var user struct {
		ID    string `xml:"id,attr"`
		Token string `xml:"authenticationToken,attr"`
	}
	if err != nil || len(data) > 64<<10 || xml.Unmarshal(data, &user) != nil || user.ID != profile.ID || user.Token == "" {
		return "", errHome
	}
	return user.Token, nil
}

// chooseHome authenticates the selected viewer before server grants are read.
// The linking account remains available only to this attempt and private state.
func (d *serverDiscovery) chooseHome(ctx context.Context, i connection.Interaction, user accountIdentity, remembered string) error {
	d.owner = d.account.Session
	d.profile = nil
	d.avatars = nil
	if !user.Home && !user.Protected {
		return nil
	}
	profiles, avatars, err := d.account.homeProfiles(ctx)
	if err != nil {
		return err
	}
	d.avatars = avatars
	selected := 0
	for j, p := range profiles {
		if p.ID == remembered {
			selected = j
		}
	}
	auto := !i.SelectProfile && (len(profiles) == 1 || (remembered != "" && profiles[selected].ID == remembered && !i.SelectServer && !i.NewAccount))
	prompt := connection.ProfilePrompt{Profiles: profiles, Avatars: avatars, Selected: selected}
	if auto && profiles[selected].Protected {
		prompt.PIN = true
	}
	for {
		choice := connection.ProfileSelection{ID: profiles[selected].ID}
		if !auto || profiles[selected].Protected {
			if i.ChooseProfile == nil {
				return errHome
			}
			choice, err = i.ChooseProfile(ctx, prompt)
			if err != nil {
				return err
			}
		}
		auto = false
		selected = -1
		for j, p := range profiles {
			if p.ID == choice.ID {
				selected = j
				break
			}
		}
		if selected < 0 {
			return errHome
		}
		profile := profiles[selected]
		token, e := d.account.switchHome(ctx, profile, choice.PIN)
		choice.PIN = ""
		if e != nil {
			if Rejected(e) && profile.Protected {
				prompt = connection.ProfilePrompt{Profiles: profiles, Avatars: avatars, Selected: selected, PIN: true, Message: messagePINIncorrect}
				continue
			}
			return e
		}
		d.profile = &profile
		d.account.Session.Token, d.account.Session.UserID = token, profile.ID
		return nil
	}
}
