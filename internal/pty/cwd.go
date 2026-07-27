package pty

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// cwdTTL is how long a looked-up working directory is trusted. `cd` happens
// at human speed, and on macOS the lookup costs a process spawn, so re-reading
// it on every 250ms status tick would be wasteful for no visible gain.
const cwdTTL = 1500 * time.Millisecond

// Cwd returns the working directory of the pane's own process — the shell, so
// it follows `cd`. Empty if it can't be determined.
//
// This is what lets a workspace's name track the directory you are actually
// in instead of the one it was opened in.
func (a *Actor) Cwd() string {
	if a.cmd.Process == nil {
		return ""
	}

	a.cwdMu.Lock()
	defer a.cwdMu.Unlock()
	if time.Since(a.cwdAt) < cwdTTL {
		return a.cwd
	}
	a.cwdAt = time.Now()
	if dir := processCwd(a.cmd.Process.Pid); dir != "" {
		a.cwd = dir
	}
	return a.cwd
}

// processCwd reads a process's working directory. Linux has it as a symlink
// in /proc; macOS has no /proc, and the syscall that would answer this
// (proc_pidinfo with PROC_PIDVNODEPATHINFO) needs cgo — so lsof it is, which
// is why the result is cached rather than polled.
func processCwd(pid int) string {
	if runtime.GOOS == "linux" {
		dir, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
		if err != nil {
			return ""
		}
		return dir
	}

	out, err := exec.Command("lsof", "-a", "-d", "cwd", "-p", strconv.Itoa(pid), "-Fn").Output()
	if err != nil {
		return ""
	}
	// -F output is one field per line, prefixed by its type letter; the cwd
	// arrives as an "n" line.
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n/") {
			return strings.TrimPrefix(line, "n")
		}
	}
	return ""
}
