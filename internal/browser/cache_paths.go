package browser

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
)

// accountCacheDir isolates servers and users without storing access tokens.
func accountCacheDir(root, server, user string) string {
	if root == "" || server == "" || user == "" {
		return ""
	}
	namespace := sha256.Sum256([]byte(strings.TrimRight(server, "/") + "\x00" + user))
	return filepath.Join(root, fmt.Sprintf("%x", namespace))
}
