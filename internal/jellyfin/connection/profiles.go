package connection

import (
	"context"
	"crypto/rand"
	"errors"
	"image"

	"mistervision/internal/connection"
	"mistervision/internal/jellyfin"
)

var errProfileSelection = errors.New("invalid Jellyfin user selection")

// chooseUser offers only independently authenticated users for this server.
// An API key never authorizes switching arbitrary server users.
func (c *Connector) chooseUser(ctx context.Context, interaction connection.Interaction, config jellyfin.Config, saved jellyfin.Session, users *userStore) (jellyfin.Session, error) {
	if interaction.ProfileAction == connection.ProfileUnchanged || config.APIKey != "" {
		return saved, nil
	}
	if interaction.ProfileAction == connection.ProfileAdd {
		return newUserSession(saved), nil
	}
	if interaction.ProfileAction != connection.ProfileChoose || interaction.ChooseProfile == nil {
		return saved, errProfileSelection
	}
	// Picker focus is independent of the authenticated viewer.
	highlightedID := saved.UserID
	for {
		prompt := connection.ProfilePrompt{AddUser: true, Forget: true}
		avatars := make(userAvatars)
		for _, user := range *users {
			if !sameUserServer(user.Session, saved) {
				continue
			}
			if user.User.ID == highlightedID {
				prompt.Selected = len(prompt.Profiles)
			}
			prompt.Profiles = append(prompt.Profiles, connection.Profile{ID: user.User.ID, Name: user.User.Name, AvatarKey: user.User.PrimaryImageTag})
			client := jellyfin.NewClient(config, user.Session)
			client.Version = c.Version
			avatars[user.User.ID] = userAvatar{client: client, tag: user.User.PrimaryImageTag}
		}
		prompt.Avatars = avatars
		if len(prompt.Profiles) == 0 {
			if saved.UserID == "" {
				return newUserSession(saved), nil
			}
			return saved, &connectionError{connectionSession, errUserState}
		}
		if len(prompt.Profiles) == 1 && prompt.Profiles[0].ID == saved.UserID {
			return saved, nil
		}
		selected, err := interaction.ChooseProfile(ctx, prompt)
		if err != nil {
			return saved, err
		}
		if err := ctx.Err(); err != nil {
			return saved, err
		}
		if selected.Action == connection.ProfileAdd && selected.ID == "" && selected.PIN == "" {
			return newUserSession(saved), nil
		}
		user, ok := users.find(saved, selected.ID)
		if !ok || selected.PIN != "" {
			return saved, errProfileSelection
		}
		switch selected.Action {
		case connection.ProfileForget:
			removed, err := c.forgetUser(ctx, interaction, saved, users, selected.ID)
			if err != nil {
				return saved, err
			}
			highlightedID = selected.ID
			if removed {
				highlightedID = saved.UserID
			}
			// Rebuild the choices after removal or a declined confirmation.
			continue
		case connection.ProfileUnchanged, connection.ProfileChoose:
			// Follow only the endpoint verified by the caller, retaining this user's credentials.
			user.Session.Server, user.Session.ServerID = saved.Server, saved.ServerID
			return user.Session, nil
		default:
			return saved, errProfileSelection
		}
	}
}

// userAvatar reads one user's image with that user's own credentials.
type userAvatar struct {
	client *jellyfin.Client
	tag    string
}

// userAvatars is an immutable, per-attempt credential map. Its keys are limited
// to identities offered in the picker. Requests never use another user's token.
type userAvatars map[string]userAvatar

// Load implements connection.ProfileAvatars without exposing tokens to the UI.
func (a userAvatars) Load(ctx context.Context, id string) (image.Image, error) {
	user, ok := a[id]
	if !ok {
		return nil, errProfileSelection
	}
	return user.client.UserAvatar(ctx, user.tag)
}

// newUserSession keeps the server identity but never borrows another user's token or device.
func newUserSession(saved jellyfin.Session) jellyfin.Session {
	return jellyfin.Session{Server: saved.Server, ServerID: saved.ServerID, DeviceID: rand.Text()}
}
