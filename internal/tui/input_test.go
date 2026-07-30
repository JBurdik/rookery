package tui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestParseKittyKey(t *testing.T) {
	tests := []struct {
		name    string
		seq     string
		key     tea.KeyMsg
		hasKey  bool
		release bool
	}{
		{"ctrl b", "\x1b[98;5u", tea.KeyMsg{Type: tea.KeyCtrlB}, true, false},
		{"alt ctrl b", "\x1b[98;7u", tea.KeyMsg{Type: tea.KeyCtrlB, Alt: true}, true, false},
		{"shifted text", "\x1b[97;2;65u", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")}, true, false},
		{"alternate keys", "\x1b[99:67:99;5u", tea.KeyMsg{Type: tea.KeyCtrlC}, true, false},
		{"modify other keys ctrl enter", "\x1b[27;5;13~", tea.KeyMsg{Type: tea.KeyEnter}, true, false},
		{"release", "\x1b[98;5:3u", tea.KeyMsg{}, false, true},
		{"private-use key", "\x1b[57358;1u", tea.KeyMsg{}, false, false},
		{"control reply", "\x1b[?1u", tea.KeyMsg{}, false, false},
		{"malformed", "\x1b[nopeu", tea.KeyMsg{}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, release, ok := parseEnhancedKey([]byte(tt.seq))
			if ok != (tt.name != "control reply" && tt.name != "malformed") {
				t.Fatalf("parseEnhancedKey ok = %v", ok)
			}
			if release != tt.release || got.hasKey != tt.hasKey || !reflect.DeepEqual(got.key, tt.key) {
				t.Fatalf("parseEnhancedKey(%q) = %#v, release=%v; want key=%#v hasKey=%v release=%v", tt.seq, got, release, tt.key, tt.hasKey, tt.release)
			}
		})
	}
}

func TestInputBridgePreservesPasteAndFiltersModernControl(t *testing.T) {
	var got []tea.Msg
	b := newInputBridge(nil, func(msg tea.Msg) { got = append(got, msg) })

	if out := string(b.process([]byte("a\x1b[200~paste\x1b[201~b\x1b[?1u\x1b[98;5u\x1b[98;5:3u"))); out != "a\x1b[200~paste\x1b[201~b" {
		t.Fatalf("process output = %q", out)
	}
	if len(got) != 1 {
		t.Fatalf("sent %d enhanced messages, want 1", len(got))
	}
	msg, ok := got[0].(enhancedKeyMsg)
	if !ok || !msg.hasKey || msg.key.Type != tea.KeyCtrlB || string(msg.data) != "\x1b[98;5u" {
		t.Fatalf("enhanced message = %#v", got[0])
	}
}

func TestInputBridgeHandlesSplitKittySequence(t *testing.T) {
	var got []tea.Msg
	b := newInputBridge(nil, func(msg tea.Msg) { got = append(got, msg) })
	if out := b.process([]byte("x\x1b[98;")); string(out) != "x" {
		t.Fatalf("first process output = %q", out)
	}
	if out := b.process([]byte("5uy")); string(out) != "y" {
		t.Fatalf("second process output = %q", out)
	}
	if len(got) != 1 {
		t.Fatalf("sent %d enhanced messages, want 1", len(got))
	}
}
