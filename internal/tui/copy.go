package tui

import (
	"encoding/base64"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jirkab/rookery/internal/attachproto"
)

// Copy mode is the client half of the daemon's scroll viewport: the daemon
// owns the cursor and the selection and draws them into the frame, this side
// only translates keys into movements and puts the yanked text on the
// clipboard.
//
// The clipboard is reached with OSC 52 rather than a clipboard library
// because it is this process that owns a terminal, and an escape sequence is
// the only route that also works when that terminal is at the other end of an
// SSH connection — which, for a multiplexer whose whole point is surviving
// detach, is the case that matters.

// enterCopyMode asks the daemon to scroll the focused pane back, and flips
// the local flag immediately so the next keystroke is already a movement.
func (m *model) enterCopyMode() {
	m.copyMode = true
	m.act(attachproto.ActionScrollMode, "", "")
}

func (m *model) exitCopyMode() {
	m.copyMode, m.selecting = false, false
	m.act(attachproto.ActionScrollExit, "", "")
	m.statusMsg = ""
}

// handleCopyKey drives the scroll viewport. The bindings are less/vim's,
// which is what anything that scrolls in a terminal uses.
func (m *model) handleCopyKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q", "ctrl+c":
		m.exitCopyMode()
	case "j", "down":
		m.scroll("down")
	case "k", "up":
		m.scroll("up")
	case "ctrl+d", "pgdown", "f":
		m.scroll("page_down")
	case "ctrl+u", "pgup", "b":
		m.scroll("page_up")
	case "g", "home":
		m.scroll("top")
	case "G", "end":
		m.scroll("bottom")
	case "v", " ":
		m.act(attachproto.ActionCopySelect, "", "")
	case "y", "enter":
		m.act(attachproto.ActionCopyYank, "", "")
		m.copyMode, m.selecting = false, false
	default:
		// Anything else means you are done reading — most often after
		// scrolling with the wheel, where nothing announced that keys had
		// changed meaning. Leave the mode and let the keystroke through
		// rather than swallowing it.
		m.exitCopyMode()
		if data := keyToBytes(msg); len(data) > 0 {
			m.send(attachproto.Input{Type: attachproto.TypeInput, Data: string(data)})
		}
	}
	return m, nil
}

func (m *model) scroll(what string) {
	m.act(attachproto.ActionScroll, "", what)
}

// copyHint is the status line while copy mode is on. It is the only place the
// bindings are written down at the moment you need them.
func (m *model) copyHint() string {
	if m.selecting {
		return "copy — j/k move · y copy selection · v clear · esc exit"
	}
	return "copy — j/k g/G move · v start selection · y copy line · esc exit"
}

// copyToClipboard writes an OSC 52 sequence past the renderer, the same way
// the bell does: it is a non-printing control sequence, so it cannot disturb
// what Bubble Tea thinks is on screen.
func copyToClipboard(text string) tea.Cmd {
	return func() tea.Msg {
		enc := base64.StdEncoding.EncodeToString([]byte(text))
		_, _ = os.Stdout.WriteString("\x1b]52;c;" + enc + "\a")
		return nil
	}
}

// copiedMsg describes what just went to the clipboard, for the status line.
func copiedMsg(text string) string {
	n := strings.Count(text, "\n") + 1
	if n == 1 {
		return "copied 1 line to the clipboard"
	}
	return "copied " + strconv.Itoa(n) + " lines to the clipboard"
}
