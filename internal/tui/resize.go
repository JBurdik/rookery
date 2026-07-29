package tui

import tea "github.com/charmbracelet/bubbletea"

// Resize mode repeats hjkl/arrow presses as divider nudges without needing
// to hold prefix+shift for each step. Unlike copy mode, there is nothing for
// the daemon to track here — every keystroke is just the same ResizePane
// frame prefix+shift+H/J/K/L already sends, so the mode lives entirely on
// this side.

func (m *model) enterResizeMode() {
	m.resizeMode = true
}

func (m *model) exitResizeMode() {
	m.resizeMode = false
	m.statusMsg = ""
}

func (m *model) handleResizeKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "enter", "q", "ctrl+c":
		m.exitResizeMode()
	case "h", "left":
		m.resize("left")
	case "j", "down":
		m.resize("down")
	case "k", "up":
		m.resize("up")
	case "l", "right":
		m.resize("right")
	}
	return m, nil
}

// resizeHint is the status line while resize mode is on.
func (m *model) resizeHint() string {
	return "resize — h/j/k/l or arrows resize · esc/enter exit"
}
