// Package cfd wraps the cloudflared CLI.
//
// Every invocation is pinned to a profile's origin certificate via
// --origincert, and reads/writes only paths pubport controls, so the user's
// real ~/.cloudflared is never touched. The binary can be overridden with
// $PUBPORT_CLOUDFLARED (used by tests/QA to inject a stub).
package cfd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Client runs cloudflared for a single profile.
type Client struct {
	Bin        string // resolved cloudflared binary
	OriginCert string // --origincert for this profile
}

// ErrNotInstalled means the cloudflared binary could not be located.
var ErrNotInstalled = fmt.Errorf("cloudflared not found on PATH (install it, then re-run — or set PUBPORT_CLOUDFLARED to its path)")

const downloadsURL = "https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/downloads/"

// InstallHint returns platform-specific instructions for installing
// cloudflared. It is shown when the binary can't be located so the user can
// fix the one prerequisite pubport can't manage itself.
func InstallHint() string {
	var b strings.Builder
	b.WriteString("cloudflared is required but was not found.\n\nInstall it:\n")
	switch runtime.GOOS {
	case "darwin":
		b.WriteString("  • Homebrew:  brew install cloudflared\n")
		b.WriteString("  • Or download the latest Darwin release from:\n    " + downloadsURL + "\n")
	case "linux":
		b.WriteString("  • Debian/Ubuntu:  download the .deb and run  sudo dpkg -i cloudflared.deb\n")
		b.WriteString("  • RHEL/Fedora:     download the .rpm and run  sudo rpm -i cloudflared.rpm\n")
		b.WriteString("  • Or grab the standalone binary for your arch from:\n    " + downloadsURL + "\n")
	case "windows":
		b.WriteString("  • winget:  winget install --id Cloudflare.cloudflared\n")
		b.WriteString("  • Or download the Windows installer from:\n    " + downloadsURL + "\n")
	default:
		b.WriteString("  • Download the release for your platform from:\n    " + downloadsURL + "\n")
	}
	b.WriteString("\nAlready installed elsewhere? Point pubport at it:\n")
	b.WriteString("  export PUBPORT_CLOUDFLARED=/path/to/cloudflared\n")
	return b.String()
}

// New locates cloudflared (honoring $PUBPORT_CLOUDFLARED) and binds it to a
// profile's origin certificate. The binary is resolved and validated up front
// so a missing install or a bad override fails with a clear message rather than
// a cryptic exec error at first use.
func New(originCert string) (*Client, error) {
	bin := os.Getenv("PUBPORT_CLOUDFLARED")
	if bin == "" {
		bin = "cloudflared"
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("%w (looked for %q)", ErrNotInstalled, bin)
	}
	return &Client{Bin: resolved, OriginCert: originCert}, nil
}

// base returns the leading args shared by tunnel subcommands.
func (c *Client) base(sub ...string) []string {
	args := []string{"tunnel", "--origincert", c.OriginCert}
	return append(args, sub...)
}

// Login runs the interactive browser login, writing the account cert to the
// profile's origin certificate path. stdout/stderr stream to the caller.
//
// cloudflared's `tunnel login` always writes cert.pem into $HOME/.cloudflared
// and ignores both --origincert and $TUNNEL_ORIGIN_CERT (verified against
// cloudflared 2026.6.0). We redirect HOME to a private temp directory so the
// user's real ~/.cloudflared is never touched, then move the certificate into
// the profile's origin-cert path.
func (c *Client) Login(ctx context.Context, out io.Writer) error {
	tmpHome, err := os.MkdirTemp("", "pubport-login-")
	if err != nil {
		return fmt.Errorf("prepare login workspace: %w", err)
	}
	defer os.RemoveAll(tmpHome)

	cmd := exec.CommandContext(ctx, c.Bin, "tunnel", "login")
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Stdin = os.Stdin
	// HOME (unix) / USERPROFILE (windows) steer cloudflared's config dir.
	cmd.Env = append(os.Environ(), "HOME="+tmpHome, "USERPROFILE="+tmpHome)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cloudflared login: %w", err)
	}

	// Move the cert cloudflared just wrote into the profile. If a future
	// cloudflared honors the origin-cert path directly, it may already be
	// in place — treat that as success.
	src := filepath.Join(tmpHome, ".cloudflared", "cert.pem")
	if err := moveInto(src, c.OriginCert); err != nil {
		if _, statErr := os.Stat(c.OriginCert); statErr == nil {
			return nil
		}
		return fmt.Errorf("saving login certificate: %w", err)
	}
	return nil
}

// moveInto moves src to dst, creating dst's parent directory and falling back
// to a copy when src and dst live on different filesystems.
func moveInto(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

// createResult is the subset of `tunnel create -o json` we consume.
type createResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Create creates a named tunnel, writing credentials to credFile, and returns
// the tunnel UUID. It is idempotent against remote state: if a tunnel of the
// same name already exists (e.g. a prior run created it but failed before
// recording it locally), its existing UUID is resolved and returned.
func (c *Client) Create(ctx context.Context, name, credFile string) (string, error) {
	args := c.base("create", "--credentials-file", credFile, "-o", "json", name)
	out, err := c.run(ctx, args...)
	if err != nil {
		if strings.Contains(out, "already exists") {
			if id, lerr := c.findByName(ctx, name); lerr == nil && id != "" {
				return id, nil
			}
		}
		return "", fmt.Errorf("cloudflared create %q: %w: %s", name, err, strings.TrimSpace(out))
	}
	// `-o json` may surround the object with log lines — including a trailing
	// JSON warning line (e.g. "version outdated") — so pick the object that
	// actually carries a tunnel id rather than spanning first-{ to last-}.
	if id := parseCreateID(out); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("could not determine tunnel UUID from cloudflared output: %s", strings.TrimSpace(out))
}

// findByName resolves an existing tunnel's UUID from the account by name.
func (c *Client) findByName(ctx context.Context, name string) (string, error) {
	rows, err := c.List(ctx)
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		if r.Name == name {
			return r.ID, nil
		}
	}
	return "", nil
}

func parseCreateID(out string) string {
	for _, obj := range jsonObjects(out) {
		var r createResult
		if err := json.Unmarshal([]byte(obj), &r); err == nil && r.ID != "" {
			return r.ID
		}
	}
	return ""
}

// jsonObjects returns each balanced top-level {...} span in s, tracking string
// and escape state so braces inside string literals don't unbalance the scan.
func jsonObjects(s string) []string {
	var objs []string
	depth, start := 0, -1
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case ch == '\\':
				esc = true
			case ch == '"':
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 && start >= 0 {
					objs = append(objs, s[start:i+1])
					start = -1
				}
			}
		}
	}
	return objs
}

// RouteDNS creates the DNS CNAME pointing hostname at the tunnel. When
// overwrite is false and a record already exists for hostname, cloudflared
// refuses (its error contains "already exists"), which the caller treats as a
// conflict; pass overwrite=true (via --overwrite-dns) to replace it. The
// cloudflared output is included in the error so the caller can classify it.
func (c *Client) RouteDNS(ctx context.Context, name, hostname string, overwrite bool) error {
	sub := []string{"route", "dns"}
	if overwrite {
		sub = append(sub, "--overwrite-dns")
	}
	sub = append(sub, name, hostname)
	out, err := c.run(ctx, c.base(sub...)...)
	if err != nil {
		return fmt.Errorf("cloudflared route dns %s -> %s: %w: %s", name, hostname, err, strings.TrimSpace(out))
	}
	return nil
}

// Delete deletes the named tunnel from the account (force, so active
// connections don't block teardown).
func (c *Client) Delete(ctx context.Context, name string) error {
	args := c.base("delete", "-f", name)
	if _, err := c.run(ctx, args...); err != nil {
		return fmt.Errorf("cloudflared delete %q: %w", name, err)
	}
	return nil
}

// RemoteTunnel is one row of `tunnel list -o json`.
type RemoteTunnel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// List returns the tunnels cloudflared reports for the account.
func (c *Client) List(ctx context.Context) ([]RemoteTunnel, error) {
	args := c.base("list", "-o", "json")
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("cloudflared list: %w", err)
	}
	start := strings.IndexByte(out, '[')
	end := strings.LastIndexByte(out, ']')
	if start < 0 || end <= start {
		return nil, nil
	}
	var rows []RemoteTunnel
	if err := json.Unmarshal([]byte(out[start:end+1]), &rows); err != nil {
		return nil, fmt.Errorf("parse tunnel list: %w", err)
	}
	return rows, nil
}

// RunArgs builds the argv for `cloudflared tunnel run` for a managed tunnel.
// The daemon package spawns this detached.
func (c *Client) RunArgs(configFile, metricsAddr, name string) []string {
	return c.base(
		"--config", configFile,
		"--metrics", metricsAddr,
		"run", name,
	)
}

// run executes cloudflared and returns combined output.
func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
