package session

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// PrepareSocket makes path ready to bind: if a socket file already exists
// there, it dials it first — a successful dial means another daemon is live
// (refuse to start), a failed dial means the file is stale (remove it).
func PrepareSocket(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err == nil {
		conn.Close()
		return fmt.Errorf("rook server already running (socket busy at %s)", path)
	}
	return os.Remove(path)
}

// IsLive reports whether a listener currently answers on path.
func IsLive(path string) bool {
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// WritePIDFile records the current process id for a session. Advisory only
// — IsLive against the API socket is the authoritative liveness check.
func WritePIDFile(name string) error {
	return os.WriteFile(PIDPath(name), []byte(strconv.Itoa(os.Getpid())), 0o600)
}

// ReadPIDFile returns the pid recorded for a session, if any.
func ReadPIDFile(name string) (int, error) {
	data, err := os.ReadFile(PIDPath(name))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// RemovePIDFile deletes a session's pidfile, ignoring a missing file.
func RemovePIDFile(name string) {
	_ = os.Remove(PIDPath(name))
}

// Cleanup removes a session's socket and pid files. Call after a graceful
// shutdown or when reclaiming a confirmed-dead session directory.
func Cleanup(name string) {
	_ = os.Remove(APISocketPath(name))
	_ = os.Remove(ClientSocketPath(name))
	RemovePIDFile(name)
}
