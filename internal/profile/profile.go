// Package profile manages pubport profiles. One profile == one Cloudflare
// account, isolated by its own origin certificate (cert.pem) and tunnel set.
package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/reallongnguyen/pubport/internal/paths"
)

// Default is the profile used when none is given.
const Default = "default"

// ErrNotLoggedIn indicates the profile has no origin certificate yet.
var ErrNotLoggedIn = errors.New("profile is not logged in (run: pubport login)")

// Profile is a named Cloudflare account context.
type Profile struct {
	Name string
}

// New returns the Profile for name, defaulting to Default when empty.
func New(name string) Profile {
	if name == "" {
		name = Default
	}
	return Profile{Name: name}
}

// Dir is the profile's state directory.
func (p Profile) Dir() string { return paths.ProfileDir(p.Name) }

// OriginCert is the path to the profile's account certificate.
func (p Profile) OriginCert() string { return paths.OriginCert(p.Name) }

// Ensure creates the profile's directory tree.
func (p Profile) Ensure() error { return paths.EnsureProfileDirs(p.Name) }

// LoggedIn reports whether the profile has an origin certificate on disk.
func (p Profile) LoggedIn() bool {
	fi, err := os.Stat(p.OriginCert())
	return err == nil && !fi.IsDir() && fi.Size() > 0
}

// RequireLogin returns ErrNotLoggedIn unless the profile has a cert.
func (p Profile) RequireLogin() error {
	if !p.LoggedIn() {
		return fmt.Errorf("%w: profile %q", ErrNotLoggedIn, p.Name)
	}
	return nil
}

// List returns all profile names that exist on disk, sorted.
func List() ([]string, error) {
	base := filepath.Join(paths.Root(), "profiles")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Remove deletes a profile's entire state directory. The caller is
// responsible for stopping daemons and deleting remote tunnels first.
func (p Profile) Remove() error {
	if p.Name == "" {
		return errors.New("refusing to remove profile with empty name")
	}
	return os.RemoveAll(p.Dir())
}
