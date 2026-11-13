// Package credentials stores the ngp user token in the user's config directory, mode 0600.
package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Credentials is the content of credentials.json.
type Credentials struct {
	Server    string    `json:"server"`    // URL passed to 'ngp login', used for the JSON API
	ProxyURL  string    `json:"proxy_url"` // public_url from client-config, the GOPROXY entry
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Valid reports whether the token is present and not expired at now.
func (c *Credentials) Valid(now time.Time) bool {
	return c != nil && c.Token != "" && now.Before(c.ExpiresAt)
}

// DefaultPath is $NGP_CONFIG_DIR/credentials.json, else <config dir>/ngp/credentials.json,
// where the config dir is $XDG_CONFIG_HOME or ~/.config (Windows: %AppData%).
func DefaultPath() (string, error) {
	if d := os.Getenv("NGP_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "credentials.json"), nil
	}
	var base string
	switch {
	case os.Getenv("XDG_CONFIG_HOME") != "":
		base = os.Getenv("XDG_CONFIG_HOME")
	case runtime.GOOS == "windows":
		d, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		base = d
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "ngp", "credentials.json"), nil
}

// Load reads the credentials; it returns (nil, nil) when there are none.
func Load(path string) (*Credentials, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c Credentials
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s is corrupt, run 'ngp login': %w", path, err)
	}
	return &c, nil
}

// Save writes the credentials atomically with mode 0600 in a 0700 directory.
func Save(path string, c *Credentials) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".credentials-*.json") // created with mode 0600
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // no-op after a successful rename
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Delete removes the credentials; a missing file is not an error.
func Delete(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
