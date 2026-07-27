// Package pty spawns commands under a pseudo-terminal and streams their
// output to a callback, one reader goroutine per pane.
package pty

import (
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// Actor owns one spawned process's PTY.
type Actor struct {
	cmd  *exec.Cmd
	ptmx *os.File

	writeMu sync.Mutex

	// Cache for Foreground, keyed on the foreground pid — see foreground.go.
	fgMu   sync.Mutex
	fgPID  int
	fgArgv []string

	// Cache for Cwd — see cwd.go.
	cwdMu sync.Mutex
	cwd   string
	cwdAt time.Time
}

// Spawn starts cmdPath with args under a new PTY sized cols x rows, in cwd
// with the given environment (nil means inherit the daemon's env). onOutput
// is called from a dedicated goroutine for every read off the PTY; onExit is
// called exactly once when the process/PTY ends.
func Spawn(cmdPath string, args []string, cwd string, env []string, cols, rows int, onOutput func([]byte), onExit func(err error, exitCode int)) (*Actor, error) {
	cmd := exec.Command(cmdPath, args...)
	cmd.Dir = cwd
	if env != nil {
		cmd.Env = env
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}

	a := &Actor{cmd: cmd, ptmx: ptmx}

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				onOutput(chunk)
			}
			if err != nil {
				break
			}
		}
		waitErr := cmd.Wait()
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		onExit(waitErr, code)
	}()

	return a, nil
}

// Write sends bytes to the process's stdin (via the PTY master).
func (a *Actor) Write(p []byte) (int, error) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return a.ptmx.Write(p)
}

// Resize updates the PTY window size.
func (a *Actor) Resize(cols, rows int) error {
	return pty.Setsize(a.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// PID returns the child process id.
func (a *Actor) PID() int {
	if a.cmd.Process == nil {
		return 0
	}
	return a.cmd.Process.Pid
}

// ForegroundPID returns the process group currently in control of the PTY —
// the program the user is actually interacting with, which for a shell pane
// is whatever it most recently launched. Returns 0 if it can't be determined.
func (a *Actor) ForegroundPID() int {
	pgid, err := unix.IoctlGetInt(int(a.ptmx.Fd()), unix.TIOCGPGRP)
	if err != nil || pgid <= 0 {
		return 0
	}
	return pgid
}

// Busy reports whether some descendant of the pane's own program currently
// owns the terminal — i.e. a shell is running a command rather than sitting
// at its prompt.
//
// This is the kernel's own answer, not a guess, which is what makes "wait
// until this shell finishes" exact. Output activity can't do it: `sleep 30`
// prints nothing at all, and a quietness heuristic declares it finished a
// second in.
func (a *Actor) Busy() bool {
	if a.cmd.Process == nil {
		return false
	}
	fg := a.ForegroundPID()
	return fg > 0 && fg != a.cmd.Process.Pid
}

// Close terminates the process and releases the PTY.
func (a *Actor) Close() error {
	if a.cmd.Process != nil {
		_ = a.cmd.Process.Kill()
	}
	return a.ptmx.Close()
}
