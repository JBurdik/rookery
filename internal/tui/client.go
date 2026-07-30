// Package tui is the thin Bubble Tea attach client — the only package in
// this module that imports Bubble Tea/lipgloss. It runs on a real terminal,
// dials the daemon's attach socket, and renders whatever frames the daemon
// sends; it holds no pane state of its own beyond "what did the server last
// tell me".
package tui

import (
	"fmt"
	"net"
	"os"

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

	// WithReportFocus asks the terminal for focus/blur events, which is how
	// the daemon knows whether a sound will be heard or a banner is needed.
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithReportFocus()}
	if cfg.UI.MouseCapture {
		// Cell motion, not all motion: rookery only needs drags and clicks,
		// and reporting every pointer move would flood the socket.
		opts = append(opts, tea.WithMouseCellMotion())
	}
	// Bubble Tea deliberately ignores unknown CSI sequences. That is normally
	// sensible, but it includes Kitty's CSI-u keyboard events. Put a very small
	// bridge in front of its reader so enhanced keys can still reach both Rook's
	// bindings and the focused PTY.
	input := newInputBridge(os.Stdin, func(msg tea.Msg) {
		if m.program != nil {
			m.program.Send(msg)
		}
	})
	opts = append(opts, tea.WithInput(input))
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
// It covers printable runes, all Ctrl-<letter> combos (their KeyType values
// are literally the control byte, see bubbletea's key.go), and the common
// xterm special-key sequences. In particular, editor-like TUIs rely on the
// modified cursor, Home/End, and Page keys for word movement and selection.
func keyToBytes(k tea.KeyMsg) []byte {
	var out []byte
	if k.Paste {
		// Bubble Tea removes the terminal's delimiters while decoding a
		// bracketed paste. The pane is the terminal application, though, so it
		// must receive those delimiters just as it would without Rook in front
		// of it. This also keeps pasted control characters from becoming Rook
		// bindings.
		out = append(out, "\x1b[200~"...)
		out = append(out, []byte(string(k.Runes))...)
		return append(out, "\x1b[201~"...)
	}

	switch {
	case k.Type == tea.KeyRunes:
		if k.Alt {
			out = append(out, 0x1b)
		}
		out = append(out, []byte(string(k.Runes))...)
	case k.Type >= 0:
		// Named control keys (Enter, Tab, Esc, Ctrl+A..Z, ...) share their
		// KeyType value with the raw control byte they represent.
		if k.Alt {
			out = append(out, 0x1b)
		}
		out = append(out, byte(k.Type))
	default:
		if seq, ok := specialKeySeq[k.Type]; ok {
			if k.Alt && seq.alt != "" {
				out = append(out, seq.alt...)
			} else {
				out = append(out, seq.normal...)
			}
		}
	}
	return out
}

type keySequence struct {
	normal string
	alt    string
}

var specialKeySeq = map[tea.KeyType]keySequence{
	tea.KeyUp:    {"\x1b[A", "\x1b[1;3A"},
	tea.KeyDown:  {"\x1b[B", "\x1b[1;3B"},
	tea.KeyRight: {"\x1b[C", "\x1b[1;3C"},
	tea.KeyLeft:  {"\x1b[D", "\x1b[1;3D"},

	tea.KeyShiftUp:    {"\x1b[1;2A", "\x1b[1;4A"},
	tea.KeyShiftDown:  {"\x1b[1;2B", "\x1b[1;4B"},
	tea.KeyShiftRight: {"\x1b[1;2C", "\x1b[1;4C"},
	tea.KeyShiftLeft:  {"\x1b[1;2D", "\x1b[1;4D"},

	tea.KeyCtrlUp:    {"\x1b[1;5A", "\x1b[1;7A"},
	tea.KeyCtrlDown:  {"\x1b[1;5B", "\x1b[1;7B"},
	tea.KeyCtrlRight: {"\x1b[1;5C", "\x1b[1;7C"},
	tea.KeyCtrlLeft:  {"\x1b[1;5D", "\x1b[1;7D"},

	tea.KeyCtrlShiftUp:    {"\x1b[1;6A", "\x1b[1;8A"},
	tea.KeyCtrlShiftDown:  {"\x1b[1;6B", "\x1b[1;8B"},
	tea.KeyCtrlShiftRight: {"\x1b[1;6C", "\x1b[1;8C"},
	tea.KeyCtrlShiftLeft:  {"\x1b[1;6D", "\x1b[1;8D"},

	tea.KeyHome:          {"\x1b[H", "\x1b[1;3H"},
	tea.KeyEnd:           {"\x1b[F", "\x1b[1;3F"},
	tea.KeyShiftHome:     {"\x1b[1;2H", "\x1b[1;4H"},
	tea.KeyShiftEnd:      {"\x1b[1;2F", "\x1b[1;4F"},
	tea.KeyCtrlHome:      {"\x1b[1;5H", "\x1b[1;7H"},
	tea.KeyCtrlEnd:       {"\x1b[1;5F", "\x1b[1;7F"},
	tea.KeyCtrlShiftHome: {"\x1b[1;6H", "\x1b[1;8H"},
	tea.KeyCtrlShiftEnd:  {"\x1b[1;6F", "\x1b[1;8F"},

	tea.KeyPgUp:       {"\x1b[5~", "\x1b[5;3~"},
	tea.KeyPgDown:     {"\x1b[6~", "\x1b[6;3~"},
	tea.KeyCtrlPgUp:   {"\x1b[5;5~", "\x1b[5;7~"},
	tea.KeyCtrlPgDown: {"\x1b[6;5~", "\x1b[6;7~"},
	tea.KeyDelete:     {"\x1b[3~", "\x1b[3;3~"},
	tea.KeyInsert:     {"\x1b[2~", "\x1b[2;3~"},
	tea.KeyShiftTab:   {"\x1b[Z", "\x1b[1;4Z"},
	tea.KeySpace:      {" ", "\x1b "},

	tea.KeyF1:  {"\x1bOP", "\x1b[1;3P"},
	tea.KeyF2:  {"\x1bOQ", "\x1b[1;3Q"},
	tea.KeyF3:  {"\x1bOR", "\x1b[1;3R"},
	tea.KeyF4:  {"\x1bOS", "\x1b[1;3S"},
	tea.KeyF5:  {"\x1b[15~", "\x1b[15;3~"},
	tea.KeyF6:  {"\x1b[17~", "\x1b[17;3~"},
	tea.KeyF7:  {"\x1b[18~", "\x1b[18;3~"},
	tea.KeyF8:  {"\x1b[19~", "\x1b[19;3~"},
	tea.KeyF9:  {"\x1b[20~", "\x1b[20;3~"},
	tea.KeyF10: {"\x1b[21~", "\x1b[21;3~"},
	tea.KeyF11: {"\x1b[23~", "\x1b[23;3~"},
	tea.KeyF12: {"\x1b[24~", "\x1b[24;3~"},
}
