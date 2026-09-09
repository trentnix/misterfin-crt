// Configuration and query behavior are derived from MiSTerFin (CC BY-NC 4.0).
package jellyfin

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Config struct {
	Server, APIKey, Username, TVMode string
	InsecureTLS                      bool
}

var profileLine = regexp.MustCompile(`^\d+x\d+(@\d+)?$`)

func LoadConfig(path string) (Config, error) {
	c := Config{TVMode: "PAL"}
	f, err := os.Open(path)
	if err != nil {
		return c, fmt.Errorf("open configuration: %w", err)
	}
	defer f.Close()
	var values []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") || profileLine.MatchString(line) {
			continue
		}
		switch strings.ToUpper(line) {
		case "PAL", "NTSC":
			c.TVMode = strings.ToUpper(line)
		case "INSECURE_TLS":
			c.InsecureTLS = true
		case "DEBUGLOG": // Accepted for compatibility. No credential-bearing HTTP logs.
		default:
			values = append(values, line)
		}
	}
	if err := s.Err(); err != nil {
		return c, errors.New("cannot read configuration")
	}
	if len(values) < 1 || len(values) > 3 {
		return c, errors.New("configuration needs a server URL, then optional API key and username")
	}
	u, err := url.Parse(values[0])
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return c, errors.New("server must be an HTTP or HTTPS URL without credentials, query, or fragment")
	}
	c.Server = strings.TrimRight(u.String(), "/")
	if len(values) > 1 {
		c.APIKey = values[1]
	}
	if len(values) > 2 {
		c.Username = values[2]
	}
	return c, nil
}

// Session belongs only to the Go client and is bound to its server URL.
type Session struct{ Server, DeviceID, Token, UserID string }

func LoadSession(dir, server string) (Session, error) {
	s := Session{Server: server}
	data, err := os.ReadFile(filepath.Join(dir, "session.json"))
	if err == nil {
		if json.Unmarshal(data, &s) != nil {
			return Session{}, errors.New("invalid Go session file")
		}
		if s.Server == server && s.DeviceID != "" {
			return s, nil
		}
	} else if !os.IsNotExist(err) {
		return s, fmt.Errorf("read Go session: %w", err)
	}
	b := make([]byte, 16)
	if _, err = rand.Read(b); err != nil {
		return s, err
	}
	s = Session{Server: server, DeviceID: hex.EncodeToString(b)}
	return s, SaveSession(dir, s)
}

func SaveSession(dir string, s Session) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".session-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(s)
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "session.json"))
}
