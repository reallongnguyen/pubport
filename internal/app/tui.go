package app

import (
	"flag"

	"github.com/reallongnguyen/pubport/internal/cfd"
	"github.com/reallongnguyen/pubport/internal/tui"
)

// cmdTUI launches the live traffic-management dashboard.
func cmdTUI(args []string) error {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	prof := fs.String("profile", "", "profile to view")
	if err := parseArgs(fs, args); err != nil {
		return err
	}
	p, err := openProfile(*prof)
	if err != nil {
		return err
	}
	// Client is optional: the dashboard renders read-only when not logged in.
	var client *cfd.Client
	if p.LoggedIn() {
		if c, err := cfd.New(p.OriginCert()); err == nil {
			client = c
		}
	}
	return tui.Run(p.Name, client)
}
