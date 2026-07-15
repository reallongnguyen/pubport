// Package metrics scrapes a cloudflared instance's local Prometheus endpoint.
//
// Metric names have drifted across cloudflared versions, so parsing is
// deliberately forgiving: it sums all label series for a metric name and
// leaves a field as "unknown" (-1) when the series is absent, rather than
// failing the whole scrape.
package metrics

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Unknown marks a metric that the running cloudflared did not expose.
const Unknown = -1.0

// Snapshot is a point-in-time read of a tunnel's metrics.
type Snapshot struct {
	Reachable     bool
	At            time.Time
	TotalRequests float64 // counter, cumulative
	RequestErrors float64 // counter, cumulative
	Connections   float64 // active edge (HA) connections
	ActiveStreams float64 // in-flight streams
}

// Candidate metric names, most-preferred first. Different cloudflared
// versions use different spellings; we take the first present.
var (
	requestNames    = []string{"cloudflared_tunnel_total_requests", "cloudflared_tunnel_request_count"}
	errorNames      = []string{"cloudflared_tunnel_request_errors", "cloudflared_tunnel_response_by_code"}
	connectionNames = []string{"cloudflared_tunnel_ha_connections", "cloudflared_tunnel_active_connections"}
	streamNames     = []string{"cloudflared_tunnel_active_streams", "cloudflared_tunnel_concurrent_requests_per_tunnel"}
)

// Scrape fetches and parses metrics from a 127.0.0.1:<port> style address.
func Scrape(ctx context.Context, addr string) Snapshot {
	snap := Snapshot{At: time.Now(), TotalRequests: Unknown, RequestErrors: Unknown, Connections: Unknown, ActiveStreams: Unknown}
	url := "http://" + addr + "/metrics"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return snap
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return snap
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return snap
	}
	snap.Reachable = true
	sums := parse(resp.Body)
	snap.TotalRequests = firstPresent(sums, requestNames)
	snap.RequestErrors = firstPresent(sums, errorNames)
	snap.Connections = firstPresent(sums, connectionNames)
	snap.ActiveStreams = firstPresent(sums, streamNames)
	return snap
}

// RequestsPerSec computes a rate from two snapshots, or Unknown if it can't.
func RequestsPerSec(prev, cur Snapshot) float64 {
	if prev.TotalRequests == Unknown || cur.TotalRequests == Unknown {
		return Unknown
	}
	dt := cur.At.Sub(prev.At).Seconds()
	if dt <= 0 {
		return Unknown
	}
	d := cur.TotalRequests - prev.TotalRequests
	if d < 0 {
		// Counter reset (tunnel restarted); don't report a negative rate.
		return 0
	}
	return d / dt
}

// parse reads Prometheus text format and sums sample values per metric name
// (labels collapsed).
func parse(r io.Reader) map[string]float64 {
	sums := map[string]float64{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, val, ok := splitSample(line)
		if !ok {
			continue
		}
		sums[name] += val
	}
	return sums
}

// splitSample extracts the base metric name (before any '{labels}') and the
// float value from a Prometheus sample line.
func splitSample(line string) (name string, val float64, ok bool) {
	// value is the last whitespace-delimited field.
	sp := strings.LastIndexAny(line, " \t")
	if sp < 0 {
		return "", 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(line[sp+1:]), 64)
	if err != nil {
		return "", 0, false
	}
	key := strings.TrimSpace(line[:sp])
	if brace := strings.IndexByte(key, '{'); brace >= 0 {
		key = key[:brace]
	}
	return key, v, true
}

func firstPresent(sums map[string]float64, names []string) float64 {
	for _, n := range names {
		if v, ok := sums[n]; ok {
			return v
		}
	}
	return Unknown
}
