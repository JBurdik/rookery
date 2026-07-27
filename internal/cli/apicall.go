// Package cli implements rook's subcommands (serve, attach, ls, kill, ping).
package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/ndjson"
	"github.com/jirkab/rookery/internal/session"
)

// Version is reported by ping and shown in `rook ls`.
const Version = "0.2.0"

// SessionEnvVar is exported into every pane's environment by the daemon, so
// an agent running inside a pane targets its own session by default instead
// of needing --session threaded through every call it makes.
const SessionEnvVar = "ROOK_SESSION"

// PaneEnvVar names the pane a process is running in. Together with
// SessionEnvVar it is how `--current` resolves "me".
const PaneEnvVar = "ROOK_PANE"

// defaultSessionName is the session a command acts on when none is given.
func defaultSessionName() string {
	if v := os.Getenv(SessionEnvVar); v != "" {
		return v
	}
	return session.DefaultName
}

// apiCall dials a session's API socket, sends one request, and returns its
// response. Good enough for one-shot CLI commands; the daemon itself keeps
// connections open for pipelined requests.
func apiCall(sessionName string, req apiproto.Request) (apiproto.Response, error) {
	return apiCallTimeout(sessionName, req, 30*time.Second)
}

// apiCallTimeout is apiCall with an explicit read deadline. `rook wait` needs
// this: its response arrives whenever the awaited thing happens, which can be
// minutes away, and a zero timeout means "wait as long as it takes".
func apiCallTimeout(sessionName string, req apiproto.Request, timeout time.Duration) (apiproto.Response, error) {
	path := session.APISocketPath(sessionName)
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return apiproto.Response{}, fmt.Errorf("no daemon running for session %q: %w", sessionName, err)
	}
	defer conn.Close()

	if timeout > 0 {
		if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
			return apiproto.Response{}, err
		}
	}

	if err := ndjson.NewWriter(conn).WriteJSON(req); err != nil {
		return apiproto.Response{}, err
	}
	var resp apiproto.Response
	if err := ndjson.NewReader(conn).ReadJSON(&resp); err != nil {
		return apiproto.Response{}, err
	}
	return resp, nil
}

// decodeResult re-marshals a Response.Result (decoded generically as
// map[string]any by encoding/json) into a concrete struct.
func decodeResult(result any, out any) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// mustJSON marshals params for a Request. Only ever called with this
// package's own hand-built structs, so a marshal failure would be a bug in
// the calling code, not bad input — panicking surfaces that immediately.
func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
