package jellyfin

import "misterfin-crt/internal/serverstate"

// Session stores this installation's Jellyfin sign-in, bound to its server URL.
type Session = serverstate.Session

// LoadSession restores Jellyfin sign-in using the shared private session store.
// Malformed records are backed up before replacement. Calls must be serialized.
func LoadSession(dir, server string) (Session, bool, error) {
	return serverstate.LoadSession(dir, server)
}

// SaveSession atomically saves Jellyfin sign-in. Calls must be serialized.
func SaveSession(dir string, s Session) error { return serverstate.SaveSession(dir, s) }
