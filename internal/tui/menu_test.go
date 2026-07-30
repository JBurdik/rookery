package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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

func rightPress(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonRight, Action: tea.MouseActionPress}
}

func TestMenuNavigateAndActivate(t *testing.T) {
	m := testModel()
	ran := ""
	m.openMenu(5, 5, []menuItem{
		{"first", func(m *model) { ran = "first" }},
		{"second", func(m *model) { ran = "second" }},
	})

	m.handleMenuKey("down")
	if m.menuIndex != 1 {
		t.Fatalf("menuIndex = %d, want 1", m.menuIndex)
	}
	m.handleMenuKey("enter")

	if ran != "second" {
		t.Errorf("ran %q, want %q", ran, "second")
	}
	if m.menuOpen {
		t.Error("menu should close once an item runs")
	}
}

func TestMenuEscCloses(t *testing.T) {
	m := testModel()
	m.openMenu(0, 0, []menuItem{{"item", func(m *model) {}}})
	m.handleMenuKey("esc")
	if m.menuOpen {
		t.Error("esc should close the menu")
	}
}

func TestMenuHitInsideActivatesOutsideDoesNot(t *testing.T) {
	m := testModel()
	ran := false
	m.openMenu(2, 2, []menuItem{{"close pane", func(m *model) { ran = true }}})

	left, top := m.menuOrigin(m.renderMenu())
	if !m.menuHit(left+1, top+1) {
		t.Fatal("a click inside the box should be consumed")
	}
	if !ran {
		t.Error("a click on the only row should activate it")
	}

	m2 := testModel()
	m2.openMenu(2, 2, []menuItem{{"close pane", func(m *model) {}}})
	if m2.menuHit(m2.width-1, m2.height-1) {
		t.Error("a click far outside the box should not be consumed")
	}
}

func TestPaneMenuRenamePromptsWithClickedPane(t *testing.T) {
	m := testModel()
	items := paneMenu("w1:p2")
	items[0].run(m) // "rename pane"

	if !m.promptMode || m.promptAction != attachproto.ActionRenamePane || m.promptTarget != "w1:p2" {
		t.Errorf("prompt = %+v, want a rename_pane prompt targeting w1:p2", m)
	}
}

func TestRightClickOnTabOpensMenu(t *testing.T) {
	m := testModel()
	m.mouseOn = true
	m.state.Tabs = []attachproto.TabSummary{{ID: "t1", Name: "one"}}
	m.tabHits = []tabHit{{id: "t1", from: 0, to: 5}}

	m.handleMouse(rightPress(2, 0))

	if !m.menuOpen {
		t.Fatal("right-click on a tab should open the context menu")
	}
	if len(m.menuItems) != 3 {
		t.Fatalf("tab menu has %d items, want 3 (rename/close/new)", len(m.menuItems))
	}
}

func TestPaneAtHitTests(t *testing.T) {
	m := testModel()
	m.state.Panes = []attachproto.PaneSummary{
		{PaneID: "p1", X: 0, Y: 0, W: 10, H: 5},
		{PaneID: "p2", X: 10, Y: 0, W: 10, H: 5},
	}
	if got := m.paneAt(3, 2); got != "p1" {
		t.Errorf("paneAt(3,2) = %q, want p1", got)
	}
	if got := m.paneAt(15, 2); got != "p2" {
		t.Errorf("paneAt(15,2) = %q, want p2", got)
	}
	if got := m.paneAt(100, 100); got != "" {
		t.Errorf("paneAt out of bounds = %q, want empty", got)
	}
}
