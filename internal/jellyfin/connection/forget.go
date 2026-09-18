package connection

import (
	"context"
	"errors"
	"fmt"

	"mistervision/internal/connection"
	"mistervision/internal/jellyfin"
)

// forgetUser commits removal to the authoritative roster first. If clearing the
// active-session file fails, the roster still prevents reuse on the next launch.
// The signed-out result also invalidates any retained in-memory session.
func (c *Connector) forgetUser(ctx context.Context, i connection.Interaction, active jellyfin.Session, users *userStore, id string) (bool, error) {
	user, ok := users.find(active, id)
	if !ok {
		return false, errProfileSelection
	}
	accepted, err := i.Ask(ctx, connection.Confirmation{Title: "Forget user?", Message: fmt.Sprintf(messageForgetUser, user.User.Name), Accept: "Forget user"})
	if err != nil || !accepted {
		return false, err
	}
	remaining := users.without(active, id)
	if err := remaining.save(c.StateDir); err != nil {
		return false, &connectionError{connectionSession, err}
	}
	*users = remaining
	if id != active.UserID {
		return true, nil
	}
	if err := jellyfin.SaveSession(c.StateDir, newUserSession(active)); err != nil {
		return true, errors.Join(connection.ErrSignedOut, &connectionError{connectionSession, err})
	}
	return true, connection.ErrSignedOut
}
