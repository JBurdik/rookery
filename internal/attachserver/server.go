// Package attachserver runs the attach/render socket that TUI (and future
// GUI) clients connect to, detach from, and reconnect to at will.
package attachserver

import (
	"net"
	"os"
	"sync/atomic"

	"github.com/jirkab/rookery/internal/attachproto"
	"github.com/jirkab/rookery/internal/ndjson"
	"github.com/jirkab/rookery/internal/session"
	"github.com/jirkab/rookery/internal/state"
)

const sendBuffer = 64

var nextClientID atomic.Uint64

// Serve binds the attach socket for the named session and accepts
// connections in the background. The returned listener should be closed on
// shutdown.
func Serve(sessionName string, loop *state.Loop) (net.Listener, error) {
	path := session.ClientSocketPath(sessionName)
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
	id := nextClientID.Add(1)

	r := ndjson.NewReader(conn)
	w := ndjson.NewWriter(conn)

	send := make(chan any, sendBuffer)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for v := range send {
			if w.WriteJSON(v) != nil {
				return
			}
		}
	}()

	var hello attachproto.Incoming
	if err := r.ReadJSON(&hello); err != nil || hello.Type != attachproto.TypeHello {
		close(send)
		<-writerDone
		return
	}
	loop.NotifyAttachConnect(id, send, hello.Cols, hello.Rows, state.ClientTheme{
		Icons: hello.Icons, Spinner: hello.Spinner,
		Accent: hello.Accent, HeaderFG: hello.HeaderFG, Border: hello.Border,
		SpinnerColor: hello.SpinnerColor, Borders: hello.Borders, DoneColor: hello.DoneColor, Blink: hello.Blink, ManagerCmd: hello.ManagerCmd,
	})

	for {
		var msg attachproto.Incoming
		if err := r.ReadJSON(&msg); err != nil {
			break
		}
		switch msg.Type {
		case attachproto.TypeInput:
			loop.NotifyAttachInput(id, msg.Data)
		case attachproto.TypeResize:
			loop.NotifyAttachResize(id, msg.Cols, msg.Rows)
		case attachproto.TypeMouse:
			loop.NotifyAttachMouse(id, attachproto.Mouse{
				Kind: msg.Kind, Button: msg.Button, X: msg.X, Y: msg.Y,
				Alt: msg.Alt, Ctrl: msg.Ctrl, Shift: msg.Shift,
			})
		case attachproto.TypeAction:
			// A named action carries its own verb, so the daemon's command
			// handler can treat it exactly like the dedicated frames below.
			loop.NotifyAttachCmd(id, msg.Action, msg.Target, msg.Text, msg.Direction)
		case attachproto.TypeFocus:
			loop.NotifyAttachCmd(id, msg.Type, msg.PaneID, "", "")
		case attachproto.TypeNewPane:
			loop.NotifyAttachCmd(id, msg.Type, msg.Label, msg.Cmd, msg.Direction)
		case attachproto.TypeClosePane:
			loop.NotifyAttachCmd(id, msg.Type, msg.PaneID, "", "")
		case attachproto.TypeMoveFocus, attachproto.TypeResizePane:
			loop.NotifyAttachCmd(id, msg.Type, "", "", msg.Direction)
		case attachproto.TypeClientFocus:
			loop.NotifyClientFocus(id, msg.Focused)
		case attachproto.TypeZoom:
			loop.NotifyAttachCmd(id, msg.Type, "", "", "")
		}
	}

	// The loop closes the send channel, not this goroutine.
	//
	// Closing it here raced the daemon: NotifyAttachDisconnect is queued on a
	// channel, so the loop could still be mid-broadcast to a client it had not
	// yet removed — writing to a channel this side had already closed, which
	// panics and takes the whole daemon down with every pane in it. Only the
	// goroutine that writes to a channel may close it.
	loop.NotifyAttachDisconnect(id)
	<-writerDone
}
