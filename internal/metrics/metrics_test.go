package metrics

import (
	"strings"
	"testing"
	"time"
)

func TestParseSumsLabelSeriesAndSkipsComments(t *testing.T) {
	in := `# HELP cloudflared_tunnel_total_requests total
# TYPE cloudflared_tunnel_total_requests counter
cloudflared_tunnel_total_requests{conn="0"} 10
cloudflared_tunnel_total_requests{conn="1"} 5
cloudflared_tunnel_ha_connections 4
garbage line without value
`
	sums := parse(strings.NewReader(in))
	if got := sums["cloudflared_tunnel_total_requests"]; got != 15 {
		t.Errorf("total_requests = %v, want 15 (labels summed)", got)
	}
	if got := sums["cloudflared_tunnel_ha_connections"]; got != 4 {
		t.Errorf("ha_connections = %v, want 4", got)
	}
}

func TestFirstPresentFallsBackToUnknown(t *testing.T) {
	if got := firstPresent(map[string]float64{}, requestNames); got != Unknown {
		t.Errorf("absent series = %v, want Unknown", got)
	}
}

func TestRequestsPerSec(t *testing.T) {
	base := time.Now()
	prev := Snapshot{At: base, TotalRequests: 100}
	cur := Snapshot{At: base.Add(2 * time.Second), TotalRequests: 130}
	if got := RequestsPerSec(prev, cur); got != 15 {
		t.Errorf("rate = %v, want 15", got)
	}
	// Counter reset -> non-negative.
	reset := Snapshot{At: base.Add(4 * time.Second), TotalRequests: 5}
	if got := RequestsPerSec(cur, reset); got != 0 {
		t.Errorf("reset rate = %v, want 0", got)
	}
	// Unknown propagates.
	if got := RequestsPerSec(Snapshot{TotalRequests: Unknown}, cur); got != Unknown {
		t.Errorf("unknown rate = %v, want Unknown", got)
	}
}
