package tunnelcfg

import (
	"strings"
	"testing"

	"github.com/reallongnguyen/pubport/internal/registry"
)

func TestRenderIngressShape(t *testing.T) {
	got := Render(registry.Tunnel{
		Name: "app", UUID: "uuid-1", Hostname: "app.example.com", Port: 3000,
		CredFile: "/creds/app.json",
	})
	for _, want := range []string{
		"tunnel: uuid-1",
		"credentials-file: /creds/app.json",
		"- hostname: app.example.com",
		"service: http://localhost:3000",
		"service: http_status:404",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config missing %q\n%s", want, got)
		}
	}
	// The catch-all 404 must be the last ingress rule.
	if strings.Index(got, "http_status:404") < strings.Index(got, "localhost:3000") {
		t.Error("catch-all 404 must follow the hostname rule")
	}
}
