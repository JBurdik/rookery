package pty

import (
	"os/exec"
	"strconv"
	"strings"
)

// Foreground returns the pid and command line of the process group currently
// in control of the PTY — what the user is actually interacting with, which
// for a shell pane is whatever it most recently launched.
//
// The command line is read with `ps`, not from the kernel's process-name
// field, because that field holds the *interpreter*: an agent installed as a
// node script or a shell wrapper shows up as "node" or "bash", which is
// useless for recognising it. Full argv gets the real name.
//
// The result is cached and only re-read when the foreground pid changes, so
// running this on every status tick costs one exec per command the user runs,
// not one per tick per pane.
func (a *Actor) Foreground() (pid int, argv []string) {
	pid = a.ForegroundPID()
	if pid <= 0 {
		return 0, nil
	}

	a.fgMu.Lock()
	defer a.fgMu.Unlock()
	if pid == a.fgPID {
		return pid, a.fgArgv
	}
	a.fgPID, a.fgArgv = pid, readArgv(pid)
	return pid, a.fgArgv
}

func readArgv(pid int) []string {
	out, err := exec.Command("ps", "-o", "args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return nil
	}
	return strings.Fields(strings.TrimSpace(string(out)))
}
