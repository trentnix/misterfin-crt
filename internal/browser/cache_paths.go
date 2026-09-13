package browser

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// browserCacheRoot shares the C root override without sharing C cache files.
func browserCacheRoot(headless bool, kind string) string {
	root := os.Getenv("MISTERFIN_CACHE_ROOT")
	if root == "" {
		root = "/media/fat"
		if headless {
			var err error
			root, err = os.UserCacheDir()
			if err != nil {
				return ""
			}
		}
	}
	return filepath.Join(root, "misterfin-go", kind)
}

// accountCacheDir isolates servers and users without storing access tokens.
func accountCacheDir(root, server, user string) string {
	if root == "" || server == "" || user == "" {
		return ""
	}
	namespace := sha256.Sum256([]byte(strings.TrimRight(server, "/") + "\x00" + user))
	return filepath.Join(root, fmt.Sprintf("%x", namespace))
}
