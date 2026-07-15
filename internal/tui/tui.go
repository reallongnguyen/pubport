// Package tui implements the live traffic-management dashboard.
//
// It polls the profile's tunnel registry and each running tunnel's metrics on
// a ticker, and offers start/stop/delete controls. Rendering degrades to plain
// text where a metric is unavailable.
package tui

import (
	"context"
	"fmt"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/reallongnguyen/pubport/internal/cfd"
	"github.com/reallongnguyen/pubport/internal/daemon"
	"github.com/reallongnguyen/pubport/internal/metrics"
	"github.com/reallongnguyen/pubport/internal/registry"
	"github.com/reallongnguyen/pubport/internal/tunnelcfg"
)

const refreshEvery = 1500 * time.Millisecond

type row struct {
	t       registry.Tunnel
	running bool
	pid     int
	snap    metrics.Snapshot
	reqRate float64
}

type model struct {
	profile string
	client  *cfd.Client // nil if not logged in; start/delete disabled
	rows    []row
	prev    map[string]metrics.Snapshot
	cursor  int
	width   int
	height  int
	status  string
	confirm bool // pending delete confirmation
	quit    bool
}

type tickMsg time.Time
type rowsMsg []row
type actionMsg struct{ note string }

// Run launches the TUI for a profile. client may be nil when the profile is
// not logged in (view still works; mutating actions are disabled).
func Run(profileName string, client *cfd.Client) error {
	m := model{
		profile: profileName,
		client:  client,
		prev:    map[string]metrics.Snapshot{},
		status:  "s start · x stop · d delete · r refresh · q quit",
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.refresh(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// refresh reads the registry and scrapes metrics on a background goroutine.
// It never touches model state (in particular m.prev): rates are computed in
// Update, which runs single-threaded, so no shared map is written off-thread.
func (m model) refresh() tea.Cmd {
	profileName := m.profile
	return func() tea.Msg {
		reg, err := registry.Load(profileName)
		if err != nil {
			return rowsMsg(nil)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var out []row
		for _, t := range reg.List() {
			pid, running := daemon.Status(profileName, t.Name)
			r := row{t: t, running: running, pid: pid, reqRate: metrics.Unknown}
			if running {
				r.snap = metrics.Scrape(ctx, daemon.MetricsAddr(t))
			}
			out = append(out, r)
		}
		return rowsMsg(out)
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		return m, tea.Batch(m.refresh(), tick())
	case rowsMsg:
		// Compute request rates here (single-threaded) against the previous
		// scrape, then remember this scrape for the next tick.
		rows := []row(msg)
		for i := range rows {
			if rows[i].running {
				if p, ok := m.prev[rows[i].t.Name]; ok {
					rows[i].reqRate = metrics.RequestsPerSec(p, rows[i].snap)
				}
				m.prev[rows[i].t.Name] = rows[i].snap
			}
		}
		m.rows = rows
		if m.cursor >= len(m.rows) {
			m.cursor = maxInt(0, len(m.rows)-1)
		}
		return m, nil
	case actionMsg:
		m.status = msg.note
		return m, m.refresh()
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m, nil
}

func (m model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirm {
		switch msg.String() {
		case "y", "Y":
			m.confirm = false
			return m, m.deleteSelected()
		default:
			m.confirm = false
			m.status = "delete cancelled"
			return m, nil
		}
	}
	switch msg.String() {
	case "q", "ctrl+c", "esc":
		m.quit = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case "r":
		return m, m.refresh()
	case "s":
		return m, m.startSelected()
	case "x":
		return m, m.stopSelected()
	case "d":
		if m.selected() != nil {
			m.confirm = true
			m.status = fmt.Sprintf("delete %q? y/N", m.selected().t.Name)
		}
	}
	return m, nil
}

func (m model) selected() *row {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return &m.rows[m.cursor]
}

func (m model) startSelected() tea.Cmd {
	r := m.selected()
	if r == nil {
		return nil
	}
	if m.client == nil {
		return note("not logged in — run: pubport login")
	}
	prof, t, client := m.profile, r.t, m.client
	return func() tea.Msg {
		if _, err := tunnelcfg.Write(t); err != nil {
			return actionMsg{note: "config error: " + err.Error()}
		}
		if _, err := daemon.Start(client, prof, t); err != nil {
			return actionMsg{note: "start failed: " + err.Error()}
		}
		return actionMsg{note: "started " + t.Name}
	}
}

func (m model) stopSelected() tea.Cmd {
	r := m.selected()
	if r == nil {
		return nil
	}
	prof, t := m.profile, r.t
	return func() tea.Msg {
		if err := daemon.Stop(prof, t.Name); err != nil {
			return actionMsg{note: "stop failed: " + err.Error()}
		}
		return actionMsg{note: "stopped " + t.Name}
	}
}

func (m model) deleteSelected() tea.Cmd {
	r := m.selected()
	if r == nil {
		return nil
	}
	if m.client == nil {
		return note("not logged in — run: pubport login")
	}
	prof, t, client := m.profile, r.t, m.client
	return func() tea.Msg {
		_ = daemon.Stop(prof, t.Name)
		if err := client.Delete(context.Background(), t.Name); err != nil {
			return actionMsg{note: "delete failed: " + err.Error()}
		}
		reg, err := registry.Load(prof)
		if err == nil {
			_ = reg.Delete(t.Name)
		}
		return actionMsg{note: "deleted " + t.Name}
	}
}

func note(s string) tea.Cmd { return func() tea.Msg { return actionMsg{note: s} } }

// --- styles ---

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	headStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("8"))
	upStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	downStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	selStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("4"))
	footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func (m model) View() string {
	if m.quit {
		return ""
	}
	title := titleStyle.Render(fmt.Sprintf("pubport · profile:%s", m.profile))
	out := title + "\n\n"
	if len(m.rows) == 0 {
		out += "  No tunnels. Create one:  pubport up <port> -hostname <host>\n"
		out += "\n" + footerStyle.Render("  q quit")
		return out
	}
	header := fmt.Sprintf("  %-2s %-14s %-26s %-7s %-8s %-8s %-6s",
		"", "NAME", "HOSTNAME", "PORT", "REQ/S", "CONNS", "PID")
	out += headStyle.Render(header) + "\n"
	for i, r := range m.rows {
		dot := downStyle.Render("○")
		pid := "-"
		reqs, conns := "—", "—"
		if r.running {
			dot = upStyle.Render("●")
			pid = strconv.Itoa(r.pid)
			reqs = rate(r.reqRate)
			conns = gauge(r.snap.Connections)
		}
		line := fmt.Sprintf("  %-2s %-14s %-26s %-7d %-8s %-8s %-6s",
			dot, truncate(r.t.Name, 14), truncate(r.t.Hostname, 26), r.t.Port, reqs, conns, pid)
		if i == m.cursor {
			line = selStyle.Render(line)
		}
		out += line + "\n"
	}
	out += "\n" + footerStyle.Render("  "+m.status)
	return out
}

func rate(v float64) string {
	if v == metrics.Unknown {
		return "—"
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

func gauge(v float64) string {
	if v == metrics.Unknown {
		return "—"
	}
	return strconv.FormatFloat(v, 'f', 0, 64)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
