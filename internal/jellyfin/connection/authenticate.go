package connection

import (
	"context"
	"fmt"

	"mistervision/internal/connection"
	"mistervision/internal/jellyfin"
	jellyfinremote "mistervision/internal/jellyfin/remote"
)

// authenticate verifies the endpoint, selects a viewer, and authenticates before
// committing credentials. A failure leaves the previous active session intact.
func (c *Connector) authenticate(ctx context.Context, interaction connection.Interaction, config jellyfin.Config, saved jellyfin.Session, recovered bool, id string) (connection.Session, error) {
	original := saved
	if saved.Server != config.Server {
		if err := jellyfin.VerifyServer(ctx, config.Server, id); err != nil {
			return connection.Session{}, &connectionError{connectionAuthentication, err}
		}
	}
	saved.Server, saved.ServerID = config.Server, id
	var users userStore
	back := connection.BackDefault
	if config.APIKey == "" {
		var err error
		users, err = loadUsers(c.StateDir)
		if err != nil {
			return connection.Session{}, &connectionError{connectionSession, err}
		}
		if _, found := users.find(saved, saved.UserID); users != nil && !found {
			saved = newUserSession(saved)
		}
		if saved.UserID == "" && users.profileCount(saved) > 0 && interaction.ProfileAction == connection.ProfileUnchanged {
			interaction.ProfileAction = connection.ProfileChoose
		}
		saved, err = c.chooseUser(ctx, interaction, config, saved, &users)
		if err != nil {
			return connection.Session{}, err
		}
		switch interaction.ProfileAction {
		case connection.ProfileChoose:
			if users.profileCount(saved) > 0 {
				back = connection.BackProfiles
			}
		case connection.ProfileAdd:
			back = connection.BackConnection
		}
		if back != connection.BackDefault {
			progress := c.Describe(nil)
			progress.Back = back
			interaction.Show(progress)
		}
	}
	client := jellyfin.NewClient(config, saved)
	client.Version, client.Diagnostics = c.Version, c.Diagnostics
	if recovered {
		c.Diagnostics.Record("authentication.session-recovered")
	}
	expected, known := users.find(saved, saved.UserID)
	user, err := client.Authenticate(ctx, func(code string) {
		p := approval(code, recovered)
		if known {
			p.Title = "Sign in again"
			p.Message = fmt.Sprintf(messageReauthorize, expected.User.Name)
		}
		p.Back = back
		if back == connection.BackProfiles {
			p.Retry = "Choose user"
		}
		interaction.Show(p)
	})
	if err != nil {
		return connection.Session{}, &connectionError{connectionAuthentication, err}
	}
	if known && user.ID != expected.User.ID {
		accepted, err := interaction.Ask(ctx, connection.Confirmation{Title: "Use a different user?", Message: fmt.Sprintf(messageDifferentUser, expected.User.Name, user.Name), Accept: "Use this user"})
		if err != nil {
			return connection.Session{}, err
		}
		if !accepted {
			return connection.Session{}, connection.ErrCanceled
		}
	}
	if err := c.saveAuthenticated(ctx, client, user, original, &users); err != nil {
		return connection.Session{}, err
	}
	return connectedSession(client, user, users, recovered), nil
}

// saveAuthenticated saves the roster before the active-session record. API keys
// never enter the roster or saved credentials. Cancellation prevents promotion.
func (c *Connector) saveAuthenticated(ctx context.Context, client *jellyfin.Client, user jellyfin.User, original jellyfin.Session, users *userStore) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	persistent := client.Session
	apiKey := client.Config.APIKey != "" && persistent.Token == client.Config.APIKey
	if apiKey {
		persistent.Token, persistent.UserID = "", ""
	} else if client.Config.APIKey == "" && users.remember(savedUser{User: user, Session: client.Session}) {
		if err := users.save(c.StateDir); err != nil {
			return &connectionError{connectionSession, err}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if persistent != original || apiKey {
		if err := jellyfin.SaveSession(c.StateDir, persistent); err != nil {
			return &connectionError{connectionSession, err}
		}
	}
	return nil
}

// connectedSession exposes only authenticated services and public viewer data.
// Configured API-key connections keep their fixed user without profile actions.
func connectedSession(client *jellyfin.Client, user jellyfin.User, users userStore, recovered bool) connection.Session {
	result := connection.Session{Server: client, Remote: jellyfinremote.New(client), Recovered: recovered}
	if client.Config.APIKey == "" {
		result.ForgetLabel = "Forget user"
		result.Profile = &connection.Profile{ID: user.ID, Name: user.Name, AvatarKey: user.PrimaryImageTag}
		result.ProfileAction = connection.ProfileAdd
		if users.profileCount(client.Session) > 1 {
			result.ProfileAction = connection.ProfileChoose
		}
		result.Avatars = userAvatars{user.ID: {client: client, tag: user.PrimaryImageTag}}
	}
	return result
}
