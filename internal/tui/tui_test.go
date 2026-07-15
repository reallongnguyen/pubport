package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/reallongnguyen/pubport/internal/metrics"
	"github.com/reallongnguyen/pubport/internal/registry"
)

func newTestModel() model {
	return model{profile: "default", prev: map[string]metrics.Snapshot{}, status: "ready"}
}

// rowsMsg must compute request rate from the previous scrape in Update (the
// single-threaded goroutine) — this is the path the concurrency fix relies on.
func TestRowsMsgComputesRateInUpdate(t *testing.T) {
	m := newTestModel()
	tun := registry.Tunnel{Name: "a", Hostname: "a.example.com", Port: 3000}
	t0 := time.Now()

	// First scrape: no prior sample -> rate stays Unknown, snapshot remembered.
	first := rowsMsg{{t: tun, running: true, reqRate: metrics.Unknown,
		snap: metrics.Snapshot{Reachable: true, At: t0, TotalRequests: 100}}}
	m2, _ := m.Update(first)
	mm := m2.(model)
	if got := mm.rows[0].reqRate; got != metrics.Unknown {
		t.Fatalf("first sample rate = %v, want Unknown", got)
	}
	if _, ok := mm.prev["a"]; !ok {
		t.Fatal("prev snapshot not stored after first rowsMsg")
	}

	// Second scrape 2s later, +20 requests -> 10 req/s.
	second := rowsMsg{{t: tun, running: true, reqRate: metrics.Unknown,
		snap: metrics.Snapshot{Reachable: true, At: t0.Add(2 * time.Second), TotalRequests: 120}}}
	m3, _ := mm.Update(second)
	if got := m3.(model).rows[0].reqRate; got != 10 {
		t.Fatalf("rate = %v, want 10 req/s", got)
	}
}

func TestQuitKey(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q produced no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q did not produce tea.Quit")
	}
}

func TestDeleteAsksConfirm(t *testing.T) {
	m := newTestModel()
	m.rows = []row{{t: registry.Tunnel{Name: "a"}}}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if !m2.(model).confirm {
		t.Fatal("d did not enter confirm state")
	}
	// 'n' cancels.
	m3, _ := m2.(model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m3.(model).confirm {
		t.Fatal("n did not cancel confirm")
	}
}

func TestViewRendersRows(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 24
	m.rows = []row{{t: registry.Tunnel{Name: "web", Hostname: "web.example.com", Port: 8080}, running: true, pid: 42,
		snap: metrics.Snapshot{Reachable: true, Connections: 3}}}
	v := m.View()
	for _, want := range []string{"pubport", "web", "web.example.com", "8080"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q\n%s", want, v)
		}
	}
}
