package cfd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A bad $PUBPORT_CLOUDFLARED override must fail up front as ErrNotInstalled
// (via errors.Is through the wrap) so the top-level handler can print guidance.
func TestNewMissingBinaryIsErrNotInstalled(t *testing.T) {
	t.Setenv("PUBPORT_CLOUDFLARED", "/nonexistent/definitely-not-cloudflared")
	_, err := New("/tmp/cert.pem")
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("errors.Is(err, ErrNotInstalled) = false; err = %v", err)
	}
}

// cloudflared's `login` writes cert.pem into $HOME/.cloudflared regardless of
// --origincert. Login must redirect HOME so the real home is untouched and the
// cert ends up at the profile's origin-cert path.
func TestLoginRelocatesCertFromHomeCloudflared(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub uses a shell script")
	}
	dir := t.TempDir()

	// Stub cloudflared that mimics real login: write to $HOME/.cloudflared.
	stub := filepath.Join(dir, "cloudflared")
	script := "#!/bin/sh\nmkdir -p \"$HOME/.cloudflared\"\nprintf 'CERT\\n' > \"$HOME/.cloudflared/cert.pem\"\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PUBPORT_CLOUDFLARED", stub)

	// A real HOME that must remain untouched by login.
	realHome := filepath.Join(dir, "realhome")
	if err := os.MkdirAll(realHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", realHome)

	oc := filepath.Join(dir, "profile", "cert.pem")
	c, err := New(oc)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Login(context.Background(), os.Stderr); err != nil {
		t.Fatalf("Login: %v", err)
	}

	got, err := os.ReadFile(oc)
	if err != nil {
		t.Fatalf("cert not written to profile path: %v", err)
	}
	if strings.TrimSpace(string(got)) != "CERT" {
		t.Errorf("profile cert = %q, want CERT", got)
	}
	if _, err := os.Stat(filepath.Join(realHome, ".cloudflared")); !os.IsNotExist(err) {
		t.Errorf("login touched the real HOME/.cloudflared (err=%v)", err)
	}
}

// cloudflared appends a trailing JSON log line (e.g. the version-outdated
// warning) after the create object; parseCreateID must return the tunnel id,
// not choke on the two concatenated objects.
func TestParseCreateIDIgnoresTrailingJSONLog(t *testing.T) {
	out := `Creating tunnel "sndq-be"...
{
  "id": "62a2761c-25f5-4fc8-9bc0-09caec4e30d3",
  "name": "sndq-be",
  "created_at": "2026-07-13T05:42:22.33299Z",
  "connections": []
}
{"level":"warn","message":"Your version 2026.6.0 is outdated. We recommend upgrading it to 2026.7.1","time":"2026-07-13T05:42:23Z"}`
	if got := parseCreateID(out); got != "62a2761c-25f5-4fc8-9bc0-09caec4e30d3" {
		t.Fatalf("parseCreateID = %q, want the tunnel id", got)
	}
}

func TestParseCreateIDPlainObject(t *testing.T) {
	if got := parseCreateID(`{"id":"abc","name":"x"}`); got != "abc" {
		t.Errorf("parseCreateID = %q, want abc", got)
	}
	if got := parseCreateID("no json here"); got != "" {
		t.Errorf("parseCreateID = %q, want empty", got)
	}
}

// A brace inside a string value must not unbalance object extraction.
func TestJSONObjectsHandlesBraceInString(t *testing.T) {
	objs := jsonObjects(`prefix {"id":"a","msg":"has } brace"} {"level":"warn"}`)
	if len(objs) != 2 {
		t.Fatalf("got %d objects, want 2: %v", len(objs), objs)
	}
	if objs[0] != `{"id":"a","msg":"has } brace"}` {
		t.Errorf("first object mis-parsed: %q", objs[0])
	}
}

func TestInstallHintIsActionable(t *testing.T) {
	hint := InstallHint()
	for _, want := range []string{downloadsURL, "PUBPORT_CLOUDFLARED"} {
		if !strings.Contains(hint, want) {
			t.Errorf("install hint missing %q\n%s", want, hint)
		}
	}
}
