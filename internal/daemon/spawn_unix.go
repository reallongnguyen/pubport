//go:build unix || darwin || linux

package daemon

import (
	"os"
	"os/exec"
	"syscall"
)

// newDetachedCmd builds a command that runs in its own process group so it is
// not killed when the parent pubport process exits.
func newDetachedCmd(bin string, args []string) *exec.Cmd {
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

// signalProcess delivers sig to the tunnel's whole process group (the child is
// a group leader via Setpgid), so any helper processes it spawned die with it.
// It falls back to signalling just the pid if the group send fails.
func signalProcess(pid int, sig syscall.Signal) {
	if err := syscall.Kill(-pid, sig); err == nil {
		return
	}
	if proc, err := os.FindProcess(pid); err == nil {
		_ = proc.Signal(sig)
	}
}
