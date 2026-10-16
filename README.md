# pubport

**Put a local port online at a stable URL on your own domain — in one command.**

pubport drives **Cloudflare named tunnels** end to end: it logs you in, creates
the tunnel, routes DNS, runs it as a managed background daemon, shows you live
traffic, and tears the whole thing down when you're done. You get a persistent
hostname like `app.example.com`, not a throwaway link.

Under the hood it wraps the `cloudflared` CLI so you never hand-edit
`~/.cloudflared/config.yml`. All pubport state lives in its own directory
(`~/.config/pubport/`) — **pubport never reads or writes your real
`~/.cloudflared`.**

## Requirements

- [`cloudflared`](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/downloads/)
  on your `PATH` (or set `PUBPORT_CLOUDFLARED=/path/to/cloudflared`). If it's
  missing, pubport prints platform-specific install steps (e.g. `brew install
  cloudflared` on macOS).
- A Cloudflare account with a zone (domain) you control.
- Go 1.20+ to build.

## Install

### Homebrew (macOS/Linux)

```sh
brew tap reallongnguyen/pubport https://github.com/reallongnguyen/pubport
brew install reallongnguyen/pubport/pubport
```

This also pulls in `cloudflared` automatically. Upgrade later with
`brew upgrade pubport`.

### Go

Install onto your `PATH` (goes to `$(go env GOPATH)/bin`, usually `~/go/bin`)
so you can run `pubport` from anywhere:

```sh
go install github.com/reallongnguyen/pubport/cmd/pubport@latest
# or, from a clone:
go install ./cmd/pubport
```

Make sure `~/go/bin` is on your `PATH` (`export PATH="$PATH:$(go env GOPATH)/bin"`
in your shell profile if not). Verify with `which pubport`.

**Just building locally?** `go build -o pubport ./cmd/pubport` drops the binary
in the current directory — run it as `./pubport`; it is *not* on your `PATH`.

## Quick start

Two commands get you online — `up` creates the tunnel, routes DNS, and runs the
daemon in one shot:

```sh
pubport login                     # authorize a Cloudflare account (opens a browser)
pubport up 3000 app.example.com   # publish localhost:3000 at app.example.com
```

That first full hostname auto-seeds the profile's default domain, so from then
on you can use [quick mode](#quick-mode) and skip the FQDN:

```sh
pubport up 8080 api    # -> api.example.com
pubport ls             # live status and traffic
pubport tui            # live traffic-management dashboard
pubport down api       # stop the daemon (tunnel & DNS kept)
pubport rm api         # tear down: stop + delete + clean up
```

`pubport up` is idempotent — re-run it to change the port/hostname or restart a
crashed tunnel. Selectors for `logs`/`down`/`rm` accept a tunnel **name**,
**hostname**, or local **port**.

## Quick mode

Set a **default domain** once, and `pubport up` synthesizes the hostname for you
so you rarely type a full FQDN:

```sh
pubport config domain example.com   # set the zone quick-mode hostnames live under
pubport config domain               # print the current default domain

pubport up 8000            # -> p8000.example.com   (no label: p<port>)
pubport up 8000 api        # -> api.example.com     (bare label: <label>.<domain>)
pubport up 8000 api.co.io  # -> api.co.io           (any dot = full hostname, as-is)
```

The default domain is **auto-seeded** the first time you pass a full hostname:
`pubport up 3000 -hostname app.example.com` sets the profile's domain to
`example.com`, so later `pubport up <port>` calls work without a separate
`config` step. An explicit `-hostname` always overrides quick mode. The
default domain is per-profile — change it any time with `pubport config domain`.

## Commands

| Command | Purpose |
|---|---|
| `login` | Authenticate a profile with a Cloudflare account |
| `up <port> [<label\|hostname>] [-hostname <h>] [-name <n>] [-force]` | Publish a port: create/route/run (idempotent). Hostname from quick mode or `-hostname` |
| `ls` | List tunnels with status, connections, and request counts |
| `logs <sel> [-f]` | Show / follow a tunnel's log |
| `down <sel>` | Stop a tunnel's background daemon |
| `rm <sel> [-keep-remote]` | Stop, delete from Cloudflare, and clean up locally |
| `tui` | Live traffic dashboard (`s` start · `x` stop · `d` delete · `q` quit) |
| `profile ls` / `profile rm <name>` | Manage profiles |
| `config domain [<your.domain>]` | Get/set the profile's default domain for quick mode |
| `version` | Print version |

## Profiles

One **profile = one Cloudflare account**, isolated by its own account
certificate and tunnel set. Every command takes `-profile <name>` (default
`default`):

```sh
pubport login -profile work
pubport up 8080 -hostname api.work.com -profile work
pubport profile ls
```

## Layout on disk

```
~/.config/pubport/profiles/<profile>/
  cert.pem                # account certificate (from `pubport login`)
  tunnels.json            # registry: name <-> uuid <-> hostname <-> port
  creds/<name>.json       # per-tunnel credentials
  tunnels/<name>.yml      # generated cloudflared config (ingress)
  run/<name>.{pid,log}    # daemon runtime state
```

Override the root with `PUBPORT_HOME`.

## Notes

- Deleting a tunnel (`rm`) removes it from your Cloudflare account but may leave
  a dangling DNS CNAME; remove it in the Cloudflare dashboard if unused.
- `tui`/`ls` traffic figures come from `cloudflared`'s local Prometheus metrics
  endpoint; a metric shown as `—` means that cloudflared build doesn't export it.
