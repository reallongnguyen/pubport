// Package app implements the pubport command-line surface.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/reallongnguyen/pubport/internal/cfd"
	"github.com/reallongnguyen/pubport/internal/profile"
)

// Version is the pubport version, overridable at build time via -ldflags.
var Version = "0.1.0"

const usage = `pubport — publish a local port to the internet over a Cloudflare named tunnel.

USAGE:
  pubport <command> [flags]

COMMANDS:
  login    Authenticate a profile with a Cloudflare account (opens a browser)
  up       Publish a local port: create/route/run a named tunnel (idempotent)
  ls       List managed tunnels with live status and traffic
  logs     Show (or follow) a tunnel's log
  down     Stop a running tunnel's background daemon
  rm       Tear a tunnel down: stop, delete remotely, clean up local state
  tui      Live traffic-management dashboard
  profile  Manage profiles (one profile = one Cloudflare account)
  config   Set the profile's default domain for quick-mode hostnames
  version  Print the pubport version

QUICK MODE:
  With a default domain set (pubport config domain <your.domain>):
    pubport up 8000            -> p8000.<default-domain>
    pubport up 8000 api        -> api.<default-domain>
    pubport up 8000 api.co.io  -> api.co.io   (any dot = full hostname)

Global:
  -profile <name>   Profile to act on (default "default")

Run "pubport <command> -h" for command-specific flags.
All state lives under ~/.config/pubport; pubport never touches ~/.cloudflared.`

// Run dispatches a command and returns a process exit code.
func Run(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "login":
		err = cmdLogin(rest)
	case "up":
		err = cmdUp(rest)
	case "ls", "list":
		err = cmdLs(rest)
	case "logs":
		err = cmdLogs(rest)
	case "down", "stop":
		err = cmdDown(rest)
	case "rm", "delete":
		err = cmdRm(rest)
	case "tui":
		err = cmdTUI(rest)
	case "profile":
		err = cmdProfile(rest)
	case "config":
		err = cmdConfig(rest)
	case "version", "-v", "--version":
		fmt.Printf("pubport %s\n", Version)
		return 0
	case "help", "-h", "--help":
		fmt.Println(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "pubport: unknown command %q\n\n%s\n", cmd, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "pubport: %v\n", err)
		if errors.Is(err, cfd.ErrNotInstalled) {
			fmt.Fprintf(os.Stderr, "\n%s", cfd.InstallHint())
		}
		return 1
	}
	return 0
}

// openProfile resolves a profile, ensures its directories exist, and returns it.
func openProfile(name string) (profile.Profile, error) {
	p := profile.New(name)
	if err := p.Ensure(); err != nil {
		return p, fmt.Errorf("prepare profile %q: %w", p.Name, err)
	}
	return p, nil
}

// newClient builds a cloudflared client bound to a logged-in profile.
func newClient(p profile.Profile) (*cfd.Client, error) {
	if err := p.RequireLogin(); err != nil {
		return nil, err
	}
	return cfd.New(p.OriginCert())
}

func ctx() context.Context { return context.Background() }

var stdout io.Writer = os.Stdout
