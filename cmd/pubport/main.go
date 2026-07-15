// Command pubport publishes a local port to the internet over a Cloudflare
// named tunnel, owning the full lifecycle: login, create, route, run
// (as a managed background daemon), inspect, and tear down. It also ships a
// live traffic-management TUI. All state lives under ~/.config/pubport and it
// never touches the user's ~/.cloudflared.
package main

import (
	"os"

	"github.com/reallongnguyen/pubport/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:]))
}
