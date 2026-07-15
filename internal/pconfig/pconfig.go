// Package pconfig persists per-profile pubport settings (config.json).
//
// The only setting today is the default domain used to synthesize hostnames
// in quick mode (`pubport up <port>` -> p<port>.<default-domain>). It lives
// alongside the profile's other state under the pubport config root; pubport
// never touches the user's real ~/.cloudflared.
package pconfig

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/reallongnguyen/pubport/internal/paths"
)

// Config is a profile's settings.
type Config struct {
	// DefaultDomain is the zone quick-mode hostnames are built under, e.g.
	// "example.com" so `up 8000` -> "p8000.example.com". Empty means unset.
	DefaultDomain string `json:"default_domain,omitempty"`
}

// Load reads a profile's config, returning a zero Config when none exists.
func Load(profile string) (Config, error) {
	var c Config
	b, err := os.ReadFile(paths.ProfileConfig(profile))
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	if len(b) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("parse config %s: %w", paths.ProfileConfig(profile), err)
	}
	return c, nil
}

// Save writes a profile's config atomically (0600).
func Save(profile string, c Config) error {
	if err := os.MkdirAll(paths.ProfileDir(profile), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(paths.ProfileConfig(profile), b, 0o600)
}
