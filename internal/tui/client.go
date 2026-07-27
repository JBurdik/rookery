// Package tui is the thin Bubble Tea attach client — the only package in
// this module that imports Bubble Tea/lipgloss. It runs on a real terminal,
// dials the daemon's attach socket, and renders whatever frames the daemon
// sends; it holds no pane state of its own beyond "what did the server last
// tell me".
package tui

import (
	"fmt"
	"net"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jirkab/rookery/internal/attachproto"
	"github.com/jirkab/rookery/internal/config"
	"github.com/jirkab/rookery/internal/ndjson"
	"github.com/jirkab/rookery/internal/session"
)

// Run connects to the named session's attach socket and drives a full-screen
// Bubble Tea program until the user detaches (or the connection drops).
func Run(sessionName, version string) error {
	cfg, keys, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	path := session.ClientSocketPath(sessionName)
	conn, err := net.Dial("unix", path)
	if err != nil {
		return fmt.Errorf("no daemon running for session %q (start one with `rook serve`): %w", sessionName, err)
	}
	defer conn.Close()

	m := newModel(sessionName, conn, cfg, keys)
	m.clientVersion = version

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if cfg.UI.MouseCapture {
		// Cell motion, not all motion: rookery only needs drags and clicks,
		// and reporting every pointer move would flood the socket.
		opts = append(opts, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(m, opts...)
	m.program = p

	go m.readLoop()

	_, err = p.Run()
	return err
}

// readLoop decodes server->client frames and feeds them into the Bubble Tea
// program via the documented Program.Send external-driver pattern.
func (m *model) readLoop() {
	r := ndjson.NewReader(m.conn)
	for {
		var msg attachproto.ServerMsg
		if err := r.ReadJSON(&msg); err != nil {
			m.program.Send(connErrMsg{err: err})
			return
		}
		m.program.Send(msg)
	}
}

type connErrMsg struct{ err error }

// keyToBytes turns a Bubble Tea key event into the raw bytes a terminal
// program on the other end of a PTY would expect to receive.
//
// ponytail: covers printable runes, all Ctrl-<letter> combos (their KeyType
// values are literally the control byte, see bubbletea's key.go), and the
// common navigation keys. Exotic keys (function keys beyond none, some
// terminal-specific sequences) aren't mapped — acceptable gap for a v1
// coding-agent-in-a-pane use case; extend specialKeySeq if one is needed.
func keyToBytes(k tea.KeyMsg) []byte {
	var out []byte
	if k.Alt {
		out = append(out, 0x1b)
	}

	switch {
	case k.Type == tea.KeyRunes:
		out = append(out, []byte(string(k.Runes))...)
	case k.Type >= 0:
		// Named control keys (Enter, Tab, Esc, Ctrl+A..Z, ...) share their
		// KeyType value with the raw control byte they represent.
		out = append(out, byte(k.Type))
	default:
		if seq, ok := specialKeySeq[k.Type]; ok {
			out = append(out, []byte(seq)...)
		}
	}
	return out
}

var specialKeySeq = map[tea.KeyType]string{
	tea.KeyUp:       "\x1b[A",
	tea.KeyDown:     "\x1b[B",
	tea.KeyRight:    "\x1b[C",
	tea.KeyLeft:     "\x1b[D",
	tea.KeyHome:     "\x1b[H",
	tea.KeyEnd:      "\x1b[F",
	tea.KeyPgUp:     "\x1b[5~",
	tea.KeyPgDown:   "\x1b[6~",
	tea.KeyDelete:   "\x1b[3~",
	tea.KeyInsert:   "\x1b[2~",
	tea.KeySpace:    " ",
	tea.KeyShiftTab: "\x1b[Z",
}
