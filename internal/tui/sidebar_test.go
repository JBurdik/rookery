package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jirkab/rookery/internal/attachproto"
)

// sidebarModel is a sidebar with two workspaces and two agents, the second of
// each being the selected one.
func sidebarModel() *model {
	// Styles are stripped when there is no TTY, which would make every
	// assertion below pass vacuously.
	lipgloss.SetColorProfile(termenv.TrueColor)
	m := testModel()
	m.sidebarHidden = false
	m.cfg.UI.SidebarWidth = 24
	m.state = attachproto.State{
		ActiveWorkspace: "w2", Focus: "p3",
		Workspaces: []attachproto.WorkspaceSummary{
			{ID: "w1", Name: "agentic-ide", Branch: "main", Status: "idle"},
			{ID: "w2", Name: "multiplexer", Branch: "trunk", Status: "working"},
		},
		Agents: []attachproto.AgentSummary{
			{PaneID: "p2", Workspace: "agentic-ide", Title: "Fix flaky test", Status: "done", Unread: true},
			{PaneID: "p3", Workspace: "multiplexer", Title: "Opravit glitch", Status: "working"},
		},
	}
	return m
}

// selectionBG is what a banded row must be painted with, in the escape form
// lipgloss emits for the default truecolor value.
const selectionBGSeq = "48;2;50;50;73"

// TestSidebarSelectionBand: the selected workspace, its branch line and the
// focused agent are the three rows that must read as selected. Herdr does this
// with a full-width band, and the underline this replaced looked like a stray
// rule under the text rather than like "you are here".
func TestSidebarSelectionBand(t *testing.T) {
	m := sidebarModel()
	lines := m.renderSidebar(14)

	banded := map[int]bool{}
	for i, l := range lines {
		if strings.Contains(l, selectionBGSeq) {
			banded[i] = true
		}
	}
	for _, want := range []struct {
		text  string
		band  bool
		label string
	}{
		{"multiplexer", true, "the active workspace"},
		{"trunk", true, "the active workspace's branch"},
		{"Opravit glitch", true, "the focused agent"},
		{"agentic-ide", false, "an inactive workspace"},
		{"Fix flaky test", false, "an unfocused agent"},
	} {
		// A workspace name appears twice — its own row, and the heading that
		// groups its agents — so "banded" means at least one row carrying that
		// text is banded, and "not banded" means none of them is.
		found, anyBanded := false, false
		for i, l := range lines {
			if !strings.Contains(l, want.text) {
				continue
			}
			found = true
			anyBanded = anyBanded || banded[i]
		}
		switch {
		case !found:
			t.Errorf("%s (%q) is not in the sidebar at all", want.label, want.text)
		case anyBanded != want.band:
			t.Errorf("%s (%q): banded=%v, want %v", want.label, want.text, anyBanded, want.band)
		}
	}

	// The spinner is drawn as its own span, so a working agent that is also
	// selected is where a one-cell hole in the band would show up.
	for _, l := range lines {
		if !strings.Contains(l, "Opravit glitch") {
			continue
		}
		if n := strings.Count(l, selectionBGSeq); n < 3 {
			t.Errorf("the selected working agent's band has a gap in it "+
				"(%d spans carry the selection colour, want one per span)\n%s", n, l)
		}
	}
}

// TestSidebarRowWidths: every row has to be exactly the sidebar width, or the
// pane content concatenated onto it lands a column off.
func TestSidebarRowWidths(t *testing.T) {
	m := sidebarModel()
	for i, l := range m.renderSidebar(14) {
		if w := lipgloss.Width(l); w != m.sidebarWidth() {
			t.Errorf("row %d is %d columns, want %d: %q", i, w, m.sidebarWidth(), l)
		}
	}
}

// TestSidebarPanelRule: the two panels are separated by a rule. Without it the
// agents heading floated in the same field of blank rows as the workspaces, so
// the sidebar read as one list with a gap in it.
func TestSidebarPanelRule(t *testing.T) {
	m := sidebarModel()
	lines := m.renderSidebar(14)

	rule := -1
	for i, l := range lines {
		if strings.Contains(l, strings.Repeat("─", 8)) {
			rule = i
		}
	}
	if rule < 0 {
		t.Fatal("no rule between the panels")
	}
	agents := -1
	for i, l := range lines {
		if strings.Contains(l, "agents") {
			agents = i
		}
	}
	if agents != rule+1 {
		t.Errorf("the rule is at row %d and the agents heading at %d; "+
			"the rule belongs directly above the heading", rule, agents)
	}
}
