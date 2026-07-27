// Package apiserver runs the JSON-RPC control socket that agents (and
// humans, via nc -U) drive the daemon through.
package apiserver

import (
	"encoding/json"
	"log"
	"net"
	"os"

	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/ndjson"
	"github.com/jirkab/rookery/internal/session"
	"github.com/jirkab/rookery/internal/state"
)

// Serve binds the API socket for the named session and accepts connections
// in the background. The returned listener should be closed on shutdown.
func Serve(sessionName string, loop *state.Loop) (net.Listener, error) {
	path := session.APISocketPath(sessionName)
	if err := session.PrepareSocket(path); err != nil {
		return nil, err
	}

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}

	go acceptLoop(ln, loop)
	return ln, nil
}

func acceptLoop(ln net.Listener, loop *state.Loop) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handleConn(conn, loop)
	}
}

func handleConn(conn net.Conn, loop *state.Loop) {
	defer conn.Close()
	r := ndjson.NewReader(conn)
	w := ndjson.NewWriter(conn)

	for {
		var req apiproto.Request
		if err := r.ReadJSON(&req); err != nil {
			return
		}
		// `watch` is the one call that never hangs up: the connection becomes
		// an event stream, so it takes over the rest of this goroutine.
		if req.Method == "watch" {
			streamEvents(conn, w, req, loop)
			return
		}
		resp := loop.SubmitAPI(req)
		if err := w.WriteJSON(resp); err != nil {
			log.Printf("apiserver: write response: %v", err)
			return
		}
	}
}

// streamEvents turns this connection into an event feed until the client goes
// away. It writes NDJSON events, the same framing as everything else, so the
// consumer is `while read line` or `jq` with nothing special about it.
func streamEvents(conn net.Conn, w *ndjson.Writer, req apiproto.Request, loop *state.Loop) {
	var p apiproto.WatchParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			_ = w.WriteJSON(apiproto.Response{
				ID:    req.ID,
				Error: &apiproto.ErrorBody{Code: apiproto.ErrInvalidParams, Message: err.Error()},
			})
			return
		}
	}

	events := loop.AddWatcher(p.Panes, p.Statuses, p.Kinds)
	defer loop.RemoveWatcher(events)

	// Acknowledge first, so a consumer knows the stream is live rather than
	// waiting on an event that may be minutes away.
	if err := w.WriteJSON(apiproto.Response{ID: req.ID, Result: apiproto.WatchStarted{Watching: true}}); err != nil {
		return
	}

	// A read on the connection is how we learn the client hung up; the write
	// loop below would otherwise happily block forever on an empty stream.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		buf := make([]byte, 256)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case ev, open := <-events:
			if !open {
				return // daemon shutting down
			}
			if err := w.WriteJSON(ev); err != nil {
				return
			}
		case <-closed:
			return
		}
	}
}
