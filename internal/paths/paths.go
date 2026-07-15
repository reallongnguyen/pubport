// Package paths resolves the on-disk layout for pubport state.
//
// Everything pubport writes lives under a single config root
// (default ~/.config/pubport, overridable via $PUBPORT_HOME). pubport never
// reads or writes the user's real ~/.cloudflared directory.
package paths

import (
	"os"
	"path/filepath"
)

// Root returns the pubport config root, creating nothing.
//
// Resolution order: $PUBPORT_HOME, then $XDG_CONFIG_HOME/pubport, then
// ~/.config/pubport.
func Root() string {
	if h := os.Getenv("PUBPORT_HOME"); h != "" {
		return h
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "pubport")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// Last resort: a relative dir so we still never touch ~/.cloudflared.
		return ".pubport"
	}
	return filepath.Join(home, ".config", "pubport")
}

// ProfileDir returns the directory holding a single profile's state.
func ProfileDir(profile string) string {
	return filepath.Join(Root(), "profiles", profile)
}

// OriginCert is the account certificate (cert.pem) for a profile, written by
// `cloudflared tunnel login`.
func OriginCert(profile string) string {
	return filepath.Join(ProfileDir(profile), "cert.pem")
}

// Registry is the tunnels.json path for a profile.
func Registry(profile string) string {
	return filepath.Join(ProfileDir(profile), "tunnels.json")
}

// ProfileConfig is the per-profile settings file (config.json), holding the
// default domain used for quick-mode hostname synthesis.
func ProfileConfig(profile string) string {
	return filepath.Join(ProfileDir(profile), "config.json")
}

// CredsDir holds per-tunnel credentials JSON files.
func CredsDir(profile string) string {
	return filepath.Join(ProfileDir(profile), "creds")
}

// CredsFile is the credentials JSON for a named tunnel.
func CredsFile(profile, name string) string {
	return filepath.Join(CredsDir(profile), name+".json")
}

// TunnelConfig is the generated cloudflared config.yml for a named tunnel.
func TunnelConfig(profile, name string) string {
	return filepath.Join(ProfileDir(profile), "tunnels", name+".yml")
}

// RunDir holds daemon runtime state (pid/log files) for a profile.
func RunDir(profile string) string {
	return filepath.Join(ProfileDir(profile), "run")
}

// PidFile is the pid file for a running tunnel daemon.
func PidFile(profile, name string) string {
	return filepath.Join(RunDir(profile), name+".pid")
}

// LogFile is the log file for a running tunnel daemon.
func LogFile(profile, name string) string {
	return filepath.Join(RunDir(profile), name+".log")
}

// WriteFileAtomic writes data to dst atomically: it writes a temp file in the
// same directory, applies perm, and renames it over dst. The parent directory
// must already exist. Callers that persist JSON state (registry, pconfig) share
// this so a future durability change lands in one place.
func WriteFileAtomic(dst string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}

// EnsureProfileDirs creates the directory tree a profile needs.
func EnsureProfileDirs(profile string) error {
	for _, d := range []string{
		ProfileDir(profile),
		CredsDir(profile),
		filepath.Join(ProfileDir(profile), "tunnels"),
		RunDir(profile),
	} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}
