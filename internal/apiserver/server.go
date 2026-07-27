// Package apiserver runs the JSON-RPC control socket that agents (and
// humans, via nc -U) drive the daemon through.
package apiserver

import (
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
		resp := loop.SubmitAPI(req)
		if err := w.WriteJSON(resp); err != nil {
			log.Printf("apiserver: write response: %v", err)
			return
		}
	}
}
