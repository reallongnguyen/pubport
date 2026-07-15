package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/reallongnguyen/pubport/internal/cfd"
	"github.com/reallongnguyen/pubport/internal/daemon"
	"github.com/reallongnguyen/pubport/internal/metrics"
	"github.com/reallongnguyen/pubport/internal/paths"
	"github.com/reallongnguyen/pubport/internal/pconfig"
	"github.com/reallongnguyen/pubport/internal/registry"
	"github.com/reallongnguyen/pubport/internal/tunnelcfg"
)

// cmdLogin authenticates a profile against a Cloudflare account.
func cmdLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	prof := fs.String("profile", "", "profile to log in (default \"default\")")
	if err := parseArgs(fs, args); err != nil {
		return err
	}
	p, err := openProfile(*prof)
	if err != nil {
		return err
	}
	client, err := cfd.New(p.OriginCert())
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Opening a browser to authorize profile %q with Cloudflare...\n", p.Name)
	if err := client.Login(ctx(), stdout); err != nil {
		return err
	}
	if !p.LoggedIn() {
		return fmt.Errorf("login finished but no certificate was written to %s", p.OriginCert())
	}
	fmt.Fprintf(stdout, "✓ Profile %q is logged in.\n", p.Name)
	return nil
}

// cmdUp publishes a local port. It is idempotent: an existing tunnel of the
// same name is reused, its route re-provisioned, and its daemon (re)started.
func cmdUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	host := fs.String("hostname", "", "explicit public hostname (overrides quick mode), e.g. app.example.com")
	name := fs.String("name", "", "tunnel name (default: derived from hostname)")
	prof := fs.String("profile", "", "profile to use")
	noWait := fs.Bool("no-wait", false, "don't wait for the tunnel to become healthy")
	force := fs.Bool("force", false, "overwrite an existing DNS record for the hostname")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: pubport up <port> [<label|hostname>] [-hostname <h>] [-name <n>] [-profile <p>] [-force]")
	}
	if err := parseArgs(fs, args, "no-wait", "force"); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("missing local port")
	}
	port, err := strconv.Atoi(fs.Arg(0))
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %q", fs.Arg(0))
	}

	p, err := openProfile(*prof)
	if err != nil {
		return err
	}
	cfg, err := pconfig.Load(p.Name)
	if err != nil {
		return err
	}

	// Resolve the public hostname. An explicit -hostname always wins;
	// otherwise synthesize it from the optional positional arg + default domain.
	resolvedHost := strings.ToLower(strings.TrimSpace(*host))
	if resolvedHost == "" {
		resolvedHost, err = resolveHostname(port, fs.Arg(1), cfg.DefaultDomain)
		if err != nil {
			return err
		}
	}

	// Auto-seed the profile's default domain from the first full hostname used,
	// so later quick-mode `up` calls work without a separate config step.
	if cfg.DefaultDomain == "" {
		if seed := seedDomainFromHost(resolvedHost); seed != "" {
			cfg.DefaultDomain = seed
			if err := pconfig.Save(p.Name, cfg); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Set default domain for profile %q to %s (change it: pubport config domain <d>)\n", p.Name, seed)
		}
	}

	tname := *name
	if tname == "" {
		tname = deriveName(resolvedHost)
	}

	client, err := newClient(p)
	if err != nil {
		return err
	}
	reg, err := registry.Load(p.Name)
	if err != nil {
		return err
	}

	t, exists := reg.Get(tname)
	prevHost := ""
	if exists {
		prevHost = t.Hostname
	}
	if !exists {
		metricsPort, err := daemon.FreePort()
		if err != nil {
			return fmt.Errorf("allocate metrics port: %w", err)
		}
		fmt.Fprintf(stdout, "Creating tunnel %q...\n", tname)
		uuid, err := client.Create(ctx(), tname, paths.CredsFile(p.Name, tname))
		if err != nil {
			return err
		}
		t = registry.Tunnel{
			Name:        tname,
			UUID:        uuid,
			Hostname:    resolvedHost,
			Port:        port,
			MetricsPort: metricsPort,
			CredFile:    paths.CredsFile(p.Name, tname),
			ConfigFile:  paths.TunnelConfig(p.Name, tname),
			CreatedAt:   time.Now(),
		}
	} else {
		// Reuse identity; apply any changed target.
		t.Hostname = resolvedHost
		t.Port = port
		if t.MetricsPort == 0 {
			if t.MetricsPort, err = daemon.FreePort(); err != nil {
				return err
			}
		}
		if t.ConfigFile == "" {
			t.ConfigFile = paths.TunnelConfig(p.Name, tname)
		}
	}
	if _, err := tunnelcfg.Write(t); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// Route DNS before recording the tunnel: if the hostname belongs to a
	// foreign record we refuse (unless --force), and must not persist a
	// hostname we couldn't provision — otherwise the next re-up would see it as
	// "already ours" and clobber the foreign record.
	overwrite := shouldOverwriteDNS(*force, prevHost, resolvedHost)
	fmt.Fprintf(stdout, "Routing %s -> tunnel %q...\n", t.Hostname, t.Name)
	if err := client.RouteDNS(ctx(), t.Name, t.Hostname, overwrite); err != nil {
		if !overwrite && strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("hostname %s is already in use by another DNS record — re-run with --force to overwrite it", t.Hostname)
		}
		return err
	}
	if err := reg.Put(t); err != nil {
		return err
	}

	if daemon.PortAvailable(port) {
		fmt.Fprintf(stdout, "! Warning: nothing appears to be listening on localhost:%d yet.\n", port)
	}

	if pid, running := daemon.Status(p.Name, t.Name); running {
		fmt.Fprintf(stdout, "Restarting daemon (pid %d)...\n", pid)
		_ = daemon.Stop(p.Name, t.Name)
	}
	pid, err := daemon.Start(client, p.Name, t)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Started cloudflared (pid %d), log: %s\n", pid, paths.LogFile(p.Name, t.Name))

	if !*noWait {
		hctx, cancel := context.WithTimeout(ctx(), 25*time.Second)
		defer cancel()
		if err := daemon.WaitHealthy(hctx, t); err != nil {
			fmt.Fprintf(stdout, "! Tunnel started but not yet healthy: %v\n", err)
			fmt.Fprintf(stdout, "  Check: pubport logs %s\n", t.Name)
			return nil
		}
	}
	fmt.Fprintf(stdout, "\n✓ Live: https://%s  ->  http://localhost:%d\n", t.Hostname, t.Port)
	return nil
}

// cmdLs lists managed tunnels with status and traffic.
func cmdLs(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	prof := fs.String("profile", "", "profile to list")
	if err := parseArgs(fs, args); err != nil {
		return err
	}
	p, err := openProfile(*prof)
	if err != nil {
		return err
	}
	reg, err := registry.Load(p.Name)
	if err != nil {
		return err
	}
	tunnels := reg.List()
	if len(tunnels) == 0 {
		fmt.Fprintf(stdout, "No tunnels in profile %q. Create one: pubport up <port> -hostname <host>\n", p.Name)
		return nil
	}
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tNAME\tHOSTNAME\tPORT\tCONNS\tREQUESTS\tPID")
	for _, t := range tunnels {
		pid, running := daemon.Status(p.Name, t.Name)
		status, pidStr := "○ down", "-"
		conns, reqs := "-", "-"
		if running {
			status, pidStr = "● up", strconv.Itoa(pid)
			snap := metrics.Scrape(ctx(), daemon.MetricsAddr(t))
			if snap.Reachable {
				conns = metricVal(snap.Connections)
				reqs = metricVal(snap.TotalRequests)
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n", status, t.Name, t.Hostname, t.Port, conns, reqs, pidStr)
	}
	return tw.Flush()
}

// cmdLogs prints (or follows) a tunnel's log file.
func cmdLogs(args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	prof := fs.String("profile", "", "profile")
	follow := fs.Bool("f", false, "follow the log")
	if err := parseArgs(fs, args, "f"); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: pubport logs <name|hostname|port> [-f]")
	}
	p, err := openProfile(*prof)
	if err != nil {
		return err
	}
	reg, err := registry.Load(p.Name)
	if err != nil {
		return err
	}
	t, err := reg.Find(fs.Arg(0))
	if err != nil {
		return err
	}
	return tailFile(paths.LogFile(p.Name, t.Name), *follow)
}

// cmdDown stops a tunnel's daemon.
func cmdDown(args []string) error {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	prof := fs.String("profile", "", "profile")
	if err := parseArgs(fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: pubport down <name|hostname|port>")
	}
	p, err := openProfile(*prof)
	if err != nil {
		return err
	}
	reg, err := registry.Load(p.Name)
	if err != nil {
		return err
	}
	t, err := reg.Find(fs.Arg(0))
	if err != nil {
		return err
	}
	if err := daemon.Stop(p.Name, t.Name); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "✓ Stopped tunnel %q.\n", t.Name)
	return nil
}

// cmdRm tears a tunnel down completely.
func cmdRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	prof := fs.String("profile", "", "profile")
	keepRemote := fs.Bool("keep-remote", false, "keep the tunnel in the Cloudflare account (only clean up locally)")
	if err := parseArgs(fs, args, "keep-remote"); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: pubport rm <name|hostname|port> [-keep-remote]")
	}
	p, err := openProfile(*prof)
	if err != nil {
		return err
	}
	reg, err := registry.Load(p.Name)
	if err != nil {
		return err
	}
	t, err := reg.Find(fs.Arg(0))
	if err != nil {
		return err
	}
	if err := daemon.Stop(p.Name, t.Name); err != nil {
		return err
	}
	if !*keepRemote {
		client, err := newClient(p)
		if err != nil {
			return err
		}
		if err := client.Delete(ctx(), t.Name); err != nil {
			return fmt.Errorf("%w (tunnel kept locally; retry or use -keep-remote)", err)
		}
	}
	for _, f := range []string{t.ConfigFile, t.CredFile, paths.PidFile(p.Name, t.Name), paths.LogFile(p.Name, t.Name)} {
		if f != "" {
			_ = os.Remove(f)
		}
	}
	if err := reg.Delete(t.Name); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "✓ Removed tunnel %q.\n", t.Name)
	fmt.Fprintf(stdout, "  Note: the DNS record for %s may remain; delete the CNAME in the Cloudflare dashboard if unused.\n", t.Hostname)
	return nil
}

// cmdProfile manages profiles.
func cmdProfile(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: pubport profile <ls|rm> [name]")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "ls", "list":
		names, err := listProfiles()
		if err != nil {
			return err
		}
		if len(names) == 0 {
			fmt.Fprintln(stdout, "No profiles yet. Create one: pubport login -profile <name>")
			return nil
		}
		tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "PROFILE\tLOGGED IN\tTUNNELS")
		for _, n := range names {
			p, _ := openProfile(n)
			reg, _ := registry.Load(n)
			li := "no"
			if p.LoggedIn() {
				li = "yes"
			}
			count := 0
			if reg != nil {
				count = len(reg.List())
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\n", n, li, count)
		}
		return tw.Flush()
	case "rm", "delete":
		if len(rest) < 1 {
			return fmt.Errorf("usage: pubport profile rm <name>")
		}
		name := rest[0]
		p, err := openProfile(name)
		if err != nil {
			return err
		}
		reg, err := registry.Load(name)
		if err != nil {
			return err
		}
		for _, t := range reg.List() {
			_ = daemon.Stop(name, t.Name)
		}
		if err := p.Remove(); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "✓ Removed profile %q locally.\n", name)
		fmt.Fprintln(stdout, "  Note: tunnels still exist in the Cloudflare account; remove them there or via 'pubport rm' before deleting the profile next time.")
		return nil
	default:
		return fmt.Errorf("unknown profile subcommand %q (want ls|rm)", sub)
	}
}

// cmdConfig manages per-profile settings. Today the only setting is the
// default domain used for quick-mode hostname synthesis.
func cmdConfig(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: pubport config domain [<your.domain>]")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "domain":
		fs := flag.NewFlagSet("config domain", flag.ContinueOnError)
		prof := fs.String("profile", "", "profile")
		if err := parseArgs(fs, rest); err != nil {
			return err
		}
		p, err := openProfile(*prof)
		if err != nil {
			return err
		}
		cfg, err := pconfig.Load(p.Name)
		if err != nil {
			return err
		}
		if fs.NArg() == 0 {
			// Get.
			if cfg.DefaultDomain == "" {
				fmt.Fprintf(stdout, "No default domain set for profile %q. Set one: pubport config domain <your.domain>\n", p.Name)
			} else {
				fmt.Fprintln(stdout, cfg.DefaultDomain)
			}
			return nil
		}
		// Set.
		d := strings.ToLower(strings.TrimSpace(fs.Arg(0)))
		if !strings.Contains(d, ".") {
			return fmt.Errorf("invalid domain %q (expected a zone like example.com)", d)
		}
		cfg.DefaultDomain = d
		if err := pconfig.Save(p.Name, cfg); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "✓ Default domain for profile %q set to %s\n", p.Name, d)
		return nil
	default:
		return fmt.Errorf("unknown config subcommand %q (want: domain)", sub)
	}
}

func metricVal(v float64) string {
	if v == metrics.Unknown {
		return "—"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// resolveHostname synthesizes the public hostname for `up` from the optional
// positional argument, falling back to the profile's default domain (quick
// mode). The rules mirror the validated prototype:
//   - arg == ""          -> p<port>.<defaultDomain>
//   - arg has no dot      -> <arg>.<defaultDomain>
//   - arg contains a dot  -> arg as-is (a full FQDN)
//
// The first two forms need a default domain; when none is set the error points
// the user at `pubport config domain`. An explicit -hostname bypasses this.
func resolveHostname(port int, arg, defaultDomain string) (string, error) {
	arg = strings.ToLower(strings.TrimSpace(arg))
	if strings.Contains(arg, ".") {
		return arg, nil
	}
	label := arg
	if label == "" {
		label = fmt.Sprintf("p%d", port)
	}
	if defaultDomain == "" {
		return "", fmt.Errorf("no default domain set for this profile — set one with `pubport config domain <your.domain>`, or pass a full hostname (e.g. app.example.com)")
	}
	return label + "." + defaultDomain, nil
}

// seedDomainFromHost derives a default domain from a full hostname by dropping
// its first label (api.example.com -> example.com). It returns "" unless the
// remainder is itself a multi-label zone, so a bare apex or TLD is never
// seeded (example.com -> "com" is rejected). Best-effort with no public-suffix
// list; the user can always correct it with `pubport config domain`.
func seedDomainFromHost(host string) string {
	i := strings.IndexByte(host, '.')
	if i < 0 {
		return ""
	}
	seed := host[i+1:]
	if !strings.Contains(seed, ".") {
		return ""
	}
	return seed
}

// shouldOverwriteDNS decides whether `up` may replace an existing DNS record:
// only when the user forced it, or the hostname is already routed to this same
// tunnel (an idempotent re-up of our own record).
func shouldOverwriteDNS(force bool, prevHost, newHost string) bool {
	return force || (prevHost != "" && prevHost == newHost)
}

// deriveName turns a hostname into a cloudflared-safe tunnel name.
func deriveName(host string) string {
	label := host
	if i := strings.IndexByte(host, '.'); i > 0 {
		label = host[:i]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(label) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "pubport"
	}
	return out
}
