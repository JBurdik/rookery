package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKeyToBytes(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyMsg
		want string
	}{
		{"runes", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("λ")}, "λ"},
		{"bracketed paste", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\x03\nb"), Paste: true}, "\x1b[200~a\x03\nb\x1b[201~"},
		{"alt rune", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true}, "\x1bx"},
		{"ctrl c", tea.KeyMsg{Type: tea.KeyCtrlC}, "\x03"},
		{"alt ctrl c", tea.KeyMsg{Type: tea.KeyCtrlC, Alt: true}, "\x1b\x03"},
		{"backspace", tea.KeyMsg{Type: tea.KeyBackspace}, "\x7f"},
		{"ctrl right", tea.KeyMsg{Type: tea.KeyCtrlRight}, "\x1b[1;5C"},
		{"alt ctrl right", tea.KeyMsg{Type: tea.KeyCtrlRight, Alt: true}, "\x1b[1;7C"},
		{"ctrl shift left", tea.KeyMsg{Type: tea.KeyCtrlShiftLeft}, "\x1b[1;6D"},
		{"alt shift home", tea.KeyMsg{Type: tea.KeyShiftHome, Alt: true}, "\x1b[1;4H"},
		{"ctrl pgdown", tea.KeyMsg{Type: tea.KeyCtrlPgDown}, "\x1b[6;5~"},
		{"alt delete", tea.KeyMsg{Type: tea.KeyDelete, Alt: true}, "\x1b[3;3~"},
		{"f5", tea.KeyMsg{Type: tea.KeyF5}, "\x1b[15~"},
		{"alt f12", tea.KeyMsg{Type: tea.KeyF12, Alt: true}, "\x1b[24;3~"},
		{"unknown special", tea.KeyMsg{Type: tea.KeyF20}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(keyToBytes(tt.key)); got != tt.want {
				t.Errorf("keyToBytes(%s) = %q, want %q", tt.key.String(), got, tt.want)
			}
		})
	}
}
