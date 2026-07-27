// Package session resolves per-session socket/pid/log paths and manages
// session directory lifecycle (stale socket cleanup, listing).
package session

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultName is the session used when none is specified.
const DefaultName = "default"

// BaseDir returns the root directory all session directories live under,
// honoring XDG_STATE_HOME when set.
func BaseDir() string {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "rookery")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "rookery")
	}
	return filepath.Join(home, ".local", "state", "rookery")
}

// WorktreeDir is where fan-out git worktrees are created. Under the state
// directory rather than beside the repo: a fan-out is scratch space, and
// scattering sibling directories through someone's source tree is a rude thing
// for a multiplexer to do.
func WorktreeDir() string { return filepath.Join(BaseDir(), "worktrees") }

// Dir returns the directory for a named session.
func Dir(name string) string {
	return filepath.Join(BaseDir(), name)
}

// APISocketPath returns the JSON-RPC control socket path for a session.
func APISocketPath(name string) string {
	return filepath.Join(Dir(name), "api.sock")
}

// ClientSocketPath returns the attach/render socket path for a session.
func ClientSocketPath(name string) string {
	return filepath.Join(Dir(name), "client.sock")
}

// PIDPath returns the pidfile path for a session.
func PIDPath(name string) string {
	return filepath.Join(Dir(name), "rook.pid")
}

// LayoutPath returns the file a session's workspace/tab/pane tree is saved
// to. It lives in the session directory but outlives the daemon: Cleanup
// removes sockets and the pidfile, not this, which is the whole point — the
// layout has to survive the restart that `just install` forces on you.
func LayoutPath(name string) string {
	return filepath.Join(Dir(name), "layout.json")
}

// LogPath returns the daemon log file path for a session.
func LogPath(name string) string {
	return filepath.Join(Dir(name), "rook.log")
}

// EnsureDir creates the session directory (and its parents) if needed.
func EnsureDir(name string) error {
	if err := os.MkdirAll(Dir(name), 0o700); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	return nil
}

// List returns the names of session directories found under BaseDir.
func List() ([]string, error) {
	entries, err := os.ReadDir(BaseDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}
