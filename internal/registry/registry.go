// Package registry persists the set of tunnels pubport manages for a profile.
//
// The registry (tunnels.json) is pubport's source of truth for the mapping
// name <-> UUID <-> hostname <-> local port <-> metrics port. It is written
// atomically.
package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/reallongnguyen/pubport/internal/paths"
)

// Tunnel is one managed named tunnel.
type Tunnel struct {
	Name        string    `json:"name"`
	UUID        string    `json:"uuid"`
	Hostname    string    `json:"hostname"`
	Port        int       `json:"port"`         // local origin port
	MetricsPort int       `json:"metrics_port"` // 127.0.0.1:<port> for cloudflared metrics
	CredFile    string    `json:"cred_file"`
	ConfigFile  string    `json:"config_file"`
	CreatedAt   time.Time `json:"created_at"`
}

// Registry is the collection of tunnels for a single profile.
type Registry struct {
	profile string
	tunnels map[string]Tunnel
}

// Load reads the profile's registry, returning an empty one if none exists.
func Load(profile string) (*Registry, error) {
	r := &Registry{profile: profile, tunnels: map[string]Tunnel{}}
	b, err := os.ReadFile(paths.Registry(profile))
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return r, nil
	}
	var stored map[string]Tunnel
	if err := json.Unmarshal(b, &stored); err != nil {
		return nil, fmt.Errorf("parse registry %s: %w", paths.Registry(profile), err)
	}
	r.tunnels = stored
	return r, nil
}

// Get returns the tunnel by name.
func (r *Registry) Get(name string) (Tunnel, bool) {
	t, ok := r.tunnels[name]
	return t, ok
}

// Find resolves a selector that may be a tunnel name, a hostname, or a local
// port (as a string). Exactly-one match is required.
func (r *Registry) Find(selector string) (Tunnel, error) {
	if t, ok := r.tunnels[selector]; ok {
		return t, nil
	}
	var matches []Tunnel
	for _, t := range r.tunnels {
		if t.Hostname == selector || fmt.Sprintf("%d", t.Port) == selector {
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return Tunnel{}, fmt.Errorf("no tunnel matches %q", selector)
	default:
		return Tunnel{}, fmt.Errorf("%q is ambiguous (%d matches)", selector, len(matches))
	}
}

// List returns all tunnels sorted by name.
func (r *Registry) List() []Tunnel {
	out := make([]Tunnel, 0, len(r.tunnels))
	for _, t := range r.tunnels {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Put inserts or replaces a tunnel and persists the registry.
func (r *Registry) Put(t Tunnel) error {
	r.tunnels[t.Name] = t
	return r.save()
}

// Delete removes a tunnel and persists the registry.
func (r *Registry) Delete(name string) error {
	delete(r.tunnels, name)
	return r.save()
}

func (r *Registry) save() error {
	if err := os.MkdirAll(paths.ProfileDir(r.profile), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r.tunnels, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(paths.Registry(r.profile), b, 0o600)
}
