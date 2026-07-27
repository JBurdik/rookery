package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jirkab/rookery/internal/attachproto"
	"github.com/jirkab/rookery/internal/config"
	"github.com/jirkab/rookery/internal/icons"
)

func testModel() *model {
	cfg := config.DefaultConfig()
	return &model{
		width: 60, height: 20,
		cfg:           cfg,
		keys:          config.DefaultHotkeys(),
		icons:         icons.For("ascii"),
		spinner:       icons.SpinnerFor(cfg.UI.Spinner),
		p:             newPalette(cfg.UI.Colors),
		sidebarHidden: true,
	}
}

// The bar is always drawn, always one row wide, and shows the right thing in
// each of its four states.
func TestManagerBar(t *testing.T) {
	m := testModel()

	states := []struct {
		name  string
		setup func()
		want  string
	}{
		{"idle", func() {}, "ask the manager"},
		{"typing", func() { m.focusManager(); m.promptText = "hello" }, "hello"},
		{"busy", func() { m.promptMode, m.managerBusy = false, true }, "thinking"},
		{"replied", func() {
			m.handleServerMsg(attachproto.ServerMsg{Type: attachproto.TypeManagerReply, Text: "did it"})
		}, "did it"},
	}
	for _, s := range states {
		s.setup()
		bar := m.renderManagerBar()
		if !strings.Contains(bar, s.want) {
			t.Errorf("%s: bar %q missing %q", s.name, bar, s.want)
		}
		if w := lipgloss.Width(bar); w != m.width {
			t.Errorf("%s: bar is %d columns, want %d", s.name, w, m.width)
		}
		if strings.Contains(bar, "\n") {
			t.Errorf("%s: bar spans more than one row", s.name)
		}
	}
	if m.managerBusy {
		t.Error("a reply should stop the spinner")
	}

	// A long reply is cut to the width rather than wrapping the layout.
	m.managerReply = strings.Repeat("x", 500)
	if w := lipgloss.Width(m.renderManagerBar()); w != m.width {
		t.Errorf("long reply: bar is %d columns, want %d", w, m.width)
	}
}

// View must lay out exactly height rows with the bar included, or every pane
// shifts by one.
func TestViewRowCount(t *testing.T) {
	m := testModel()
	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		t.Fatalf("View drew %d rows, want %d", len(lines), m.height)
	}
	if !strings.Contains(lines[m.height-2], "ask the manager") {
		t.Errorf("manager bar not on the second-to-last row: %q", lines[m.height-2])
	}
}
