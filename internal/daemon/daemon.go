// Package daemon supervises cloudflared as a detached background process.
//
// pubport starts `cloudflared tunnel run` in its own process group so it
// outlives the short-lived CLI invocation, tracks it by pid file, and stops it
// on request. Health is confirmed via the tunnel's local metrics endpoint.
package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/reallongnguyen/pubport/internal/cfd"
	"github.com/reallongnguyen/pubport/internal/paths"
	"github.com/reallongnguyen/pubport/internal/registry"
)

// MetricsAddr is the loopback address cloudflared serves metrics on.
func MetricsAddr(t registry.Tunnel) string {
	return fmt.Sprintf("127.0.0.1:%d", t.MetricsPort)
}

// Start spawns a detached cloudflared for the tunnel and records its pid.
// It returns the child pid. It errors if the tunnel is already running.
func Start(client *cfd.Client, profile string, t registry.Tunnel) (int, error) {
	if pid, ok := Status(profile, t.Name); ok {
		return pid, fmt.Errorf("tunnel %q already running (pid %d)", t.Name, pid)
	}
	if err := os.MkdirAll(paths.RunDir(profile), 0o700); err != nil {
		return 0, err
	}
	logPath := paths.LogFile(profile, t.Name)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, fmt.Errorf("open log %s: %w", logPath, err)
	}
	defer logFile.Close()

	args := client.RunArgs(t.ConfigFile, MetricsAddr(t), t.Name)
	cmd := newDetachedCmd(client.Bin, args)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start cloudflared: %w", err)
	}
	pid := cmd.Process.Pid
	if err := writePid(profile, t.Name, pid); err != nil {
		// Best-effort cleanup: don't leave an untracked process.
		_ = cmd.Process.Kill()
		return 0, err
	}
	// Detach: we don't Wait, the child is reparented to init.
	_ = cmd.Process.Release()
	return pid, nil
}

// WaitHealthy polls the tunnel's metrics /ready endpoint until it returns 200
// or the context is done. A non-nil error means it never became healthy.
func WaitHealthy(ctx context.Context, t registry.Tunnel) error {
	url := "http://" + MetricsAddr(t) + "/ready"
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("tunnel %q did not become healthy: %w", t.Name, ctx.Err())
		case <-ticker.C:
		}
	}
}

// Stop terminates the tunnel's process (SIGTERM, then SIGKILL) and removes the
// pid file. It is a no-op if the tunnel is not running.
func Stop(profile, name string) error {
	pid, ok := Status(profile, name)
	if !ok {
		_ = os.Remove(paths.PidFile(profile, name))
		return nil
	}
	signalProcess(pid, syscall.SIGTERM)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			_ = os.Remove(paths.PidFile(profile, name))
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	signalProcess(pid, syscall.SIGKILL)
	_ = os.Remove(paths.PidFile(profile, name))
	return nil
}

// Status reports whether the tunnel's daemon is running, and its pid.
func Status(profile, name string) (int, bool) {
	pid, err := readPid(profile, name)
	if err != nil {
		return 0, false
	}
	if !alive(pid) {
		return pid, false
	}
	return pid, true
}

// PortAvailable reports whether a TCP port on loopback can be bound (used to
// pick a free metrics port and to validate the origin port isn't the tunnel's).
func PortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// FreePort asks the OS for an unused loopback TCP port.
func FreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks for existence without delivering a signal.
	return proc.Signal(syscall.Signal(0)) == nil
}

func writePid(profile, name string, pid int) error {
	return os.WriteFile(paths.PidFile(profile, name), []byte(strconv.Itoa(pid)), 0o600)
}

func readPid(profile, name string) (int, error) {
	b, err := os.ReadFile(paths.PidFile(profile, name))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}
