package tui

import (
	"bytes"
	"encoding/json"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jirkab/rookery/internal/attachproto"
	"github.com/jirkab/rookery/internal/ndjson"
)

// mouseModel is a client with one full-width pane at the top of the content
// area, whose program may or may not have asked for mouse reporting.
func mouseModel(wantsMouse bool) (*model, *bytes.Buffer) {
	m := testModel()
	m.mouseOn = true
	var buf bytes.Buffer
	m.writer = ndjson.NewWriter(&buf)
	m.state = attachproto.State{Panes: []attachproto.PaneSummary{{
		PaneID: "p1", X: 0, Y: 0, W: 60, H: 10, MouseWanted: wantsMouse,
	}}}
	return m, &buf
}

func sentMouse(t *testing.T, buf *bytes.Buffer) *attachproto.Mouse {
	t.Helper()
	if buf.Len() == 0 {
		return nil
	}
	var got attachproto.Mouse
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &got); err != nil {
		t.Fatalf("decode %q: %v", buf.String(), err)
	}
	return &got
}

// TestMouseForwardedToPane: every kind of event over the content area is
// handed to the daemon in content-area coordinates, release included.
func TestMouseForwardedToPane(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.MouseMsg
		kind string
		btn  string
	}{
		{"press", tea.MouseMsg{X: 4, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}, "press", "left"},
		{"release", tea.MouseMsg{X: 4, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}, "release", "left"},
		{"drag", tea.MouseMsg{X: 4, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion}, "drag", "left"},
		{"wheel", tea.MouseMsg{X: 4, Y: 3, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress}, "wheel", "up"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, buf := mouseModel(true)
			m.handleMouse(tt.msg)
			got := sentMouse(t, buf)
			if got == nil {
				t.Fatal("nothing sent to the daemon")
			}
			if got.Kind != tt.kind || got.Button != tt.btn {
				t.Errorf("kind/button = %q/%q, want %q/%q", got.Kind, got.Button, tt.kind, tt.btn)
			}
			if got.X != 4 || got.Y != 3-headerRows {
				t.Errorf("coords = %d,%d; want %d,%d", got.X, got.Y, 4, 3-headerRows)
			}
		})
	}
}

// TestRightClickPaneMenu: the pane context menu owns right-clicks only while
// the program in the pane has not asked for mouse reporting itself.
func TestRightClickPaneMenu(t *testing.T) {
	m, buf := mouseModel(false)
	m.handleMouse(rightPress(4, 3))
	if !m.menuOpen {
		t.Error("right-click on an ordinary pane should open its menu")
	}
	if sentMouse(t, buf) != nil {
		t.Errorf("menu click must not also reach the pane: %q", buf.String())
	}

	m, buf = mouseModel(true)
	m.handleMouse(rightPress(4, 3))
	if m.menuOpen {
		t.Error("a pane that asked for mouse reporting keeps its own right-clicks")
	}
	got := sentMouse(t, buf)
	if got == nil || got.Kind != "press" || got.Button != "right" {
		t.Errorf("forwarded %+v, want a right press", got)
	}
}
