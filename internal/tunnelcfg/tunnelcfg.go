// Package tunnelcfg generates the cloudflared config.yml for a managed tunnel.
//
// The shape mirrors the standard named-tunnel config: a tunnel UUID, a
// credentials-file, and an ingress list mapping the hostname to the local
// origin, terminated by a catch-all http_status:404 rule.
package tunnelcfg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reallongnguyen/pubport/internal/registry"
)

// Render returns the config.yml contents for a tunnel.
func Render(t registry.Tunnel) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Managed by pubport. Do not edit by hand.\n")
	fmt.Fprintf(&b, "tunnel: %s\n", t.UUID)
	fmt.Fprintf(&b, "credentials-file: %s\n", t.CredFile)
	fmt.Fprintf(&b, "\ningress:\n")
	fmt.Fprintf(&b, "  - hostname: %s\n", t.Hostname)
	fmt.Fprintf(&b, "    service: http://localhost:%d\n", t.Port)
	fmt.Fprintf(&b, "  - service: http_status:404\n")
	return b.String()
}

// Write renders and writes the config file for a tunnel (0600), creating the
// parent directory as needed. It returns the path written.
func Write(t registry.Tunnel) (string, error) {
	if t.ConfigFile == "" {
		return "", fmt.Errorf("tunnel %q has no config file path", t.Name)
	}
	if err := os.MkdirAll(filepath.Dir(t.ConfigFile), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(t.ConfigFile, []byte(Render(t)), 0o600); err != nil {
		return "", err
	}
	return t.ConfigFile, nil
}
