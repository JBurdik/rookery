package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jirkab/rookery/internal/attachproto"
)

// The right-click context menu: a short popover of actions for whatever was
// clicked — a pane, a tab, or a workspace row. Each item runs exactly what
// its own keybinding would, so the menu can't drift out of sync with the
// prefix commands; it is just another way to reach them.
type menuItem struct {
	label string
	run   func(m *model)
}

func (m *model) openMenu(x, y int, items []menuItem) {
	m.menuOpen, m.menuIndex, m.menuItems = true, 0, items
	m.menuX, m.menuY = x, y
}

func (m *model) closeMenu() {
	m.menuOpen, m.menuItems = false, nil
}

func paneMenu(paneID string) []menuItem {
	return []menuItem{
		{"rename pane", func(m *model) {
			m.startPrompt("rename pane", attachproto.ActionRenamePane, paneID)
		}},
		{"close pane", func(m *model) {
			m.send(attachproto.ClosePane{Type: attachproto.TypeClosePane, PaneID: paneID})
		}},
		{"split right", func(m *model) {
			m.focusThenAct(paneID, func() { m.send(attachproto.NewPane{Type: attachproto.TypeNewPane, Direction: "right"}) })
		}},
		{"split down", func(m *model) {
			m.focusThenAct(paneID, func() { m.send(attachproto.NewPane{Type: attachproto.TypeNewPane, Direction: "down"}) })
		}},
		{"zoom", func(m *model) {
			m.focusThenAct(paneID, func() { m.send(attachproto.Zoom{Type: attachproto.TypeZoom}) })
		}},
	}
}

func tabMenu(tabID string) []menuItem {
	return []menuItem{
		{"rename tab", func(m *model) { m.startPrompt("rename tab", attachproto.ActionRenameTab, tabID) }},
		{"close tab", func(m *model) { m.act(attachproto.ActionCloseTab, tabID, "") }},
		{"new tab", func(m *model) { m.act(attachproto.ActionNewTab, "", "") }},
	}
}

func workspaceMenu(wsID string) []menuItem {
	return []menuItem{
		{"rename workspace", func(m *model) { m.startPrompt("rename workspace", attachproto.ActionRenameWS, wsID) }},
		{"new workspace", func(m *model) { m.act(attachproto.ActionNewWorkspace, "", "") }},
		{"close workspace", func(m *model) { m.act(attachproto.ActionCloseWS, wsID, "") }},
	}
}

// focusThenAct moves focus to the pane the menu was opened on — since split
// and zoom always act on whichever pane is focused — before running the
// action itself. A pane clicked in the menu is not necessarily the one that
// was focused when the click landed.
func (m *model) focusThenAct(paneID string, act func()) {
	if paneID != m.state.Focus {
		m.send(attachproto.Focus{Type: attachproto.TypeFocus, PaneID: paneID})
	}
	act()
}

func (m *model) handleMenuKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "ctrl+c", "q":
		m.closeMenu()
	case "enter":
		m.activateMenuItem()
	case "down", "j":
		if m.menuIndex+1 < len(m.menuItems) {
			m.menuIndex++
		}
	case "up", "k":
		if m.menuIndex > 0 {
			m.menuIndex--
		}
	}
	return m, nil
}

func (m *model) activateMenuItem() {
	if m.menuIndex < 0 || m.menuIndex >= len(m.menuItems) {
		m.closeMenu()
		return
	}
	item := m.menuItems[m.menuIndex]
	m.closeMenu()
	item.run(m)
}

// menuOrigin is where the box's top-left corner lands: pinned to the click
// point, but pulled back onto the screen so the menu is never clipped by an
// edge it was opened near.
func (m *model) menuOrigin(box []string) (left, top int) {
	boxW := min(boxWidth(box)+2, m.width)
	boxH := min(len(box)+2, m.height)
	left = min(m.menuX, max(m.width-boxW, 0))
	top = min(m.menuY, max(m.height-boxH, 0))
	return left, top
}

// menuHit resolves a click against the open menu: activating the row under
// it and reporting true, or reporting false when the click landed outside
// the box entirely (the caller dismisses the menu in that case).
func (m *model) menuHit(x, y int) bool {
	box := m.renderMenu()
	left, top := m.menuOrigin(box)
	boxW := min(boxWidth(box)+2, m.width)
	boxH := min(len(box)+2, m.height)

	row, col := y-top-1, x-left-1
	if row < 0 || row >= boxH-2 || col < 0 || col >= boxW-2 {
		return false
	}
	if row < len(m.menuItems) {
		m.menuIndex = row
		m.activateMenuItem()
	}
	return true
}

// renderMenu draws the popover's contents: one line per item, the selected
// one banded — the same visual language as the goto picker.
func (m *model) renderMenu() []string {
	width := longestMenuLabel(m.menuItems) + 3
	out := make([]string, 0, len(m.menuItems))
	for i, it := range m.menuItems {
		cursor := "  "
		style := m.p.popoverBG
		if i == m.menuIndex {
			cursor, style = "❯ ", m.p.popoverTitle
		}
		out = append(out, style.Render(clampPad(cursor+it.label, width)))
	}
	return out
}

func longestMenuLabel(items []menuItem) int {
	n := 0
	for _, it := range items {
		if l := len([]rune(it.label)); l > n {
			n = l
		}
	}
	return n
}
