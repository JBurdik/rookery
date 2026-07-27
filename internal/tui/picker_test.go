package tui

import (
	"testing"

	"github.com/jirkab/rookery/internal/attachproto"
)

func TestFuzzyScoreMatchesSubsequences(t *testing.T) {
	tests := []struct {
		text, query string
		want        bool
	}{
		{"Refactor the parser", "parser", true},
		{"Refactor the parser", "rfp", true},   // initials, spread out
		{"Refactor the parser", "REFAC", true}, // case-insensitive
		{"Refactor the parser", "xyz", false},
		{"Refactor the parser", "resparx", false}, // one letter too many
		{"anything", "", true},
	}
	for _, tt := range tests {
		if _, ok := fuzzyScore(tt.text, tt.query); ok != tt.want {
			t.Errorf("fuzzyScore(%q, %q) matched = %v, want %v", tt.text, tt.query, ok, tt.want)
		}
	}
}

func TestFuzzyScorePrefersTightEarlyMatches(t *testing.T) {
	tight, _ := fuzzyScore("parser rewrite", "parser")
	spread, _ := fuzzyScore("prepare a series of runs", "parser")
	if tight >= spread {
		t.Errorf("a contiguous match (%d) should score better than a spread one (%d)", tight, spread)
	}
}

func TestPickerRanksAndActivates(t *testing.T) {
	m := &model{}
	m.state = attachproto.State{
		Agents: []attachproto.AgentSummary{
			{PaneID: "w1:p1", Title: "Fix the flaky test", Workspace: "api", Agent: "claude"},
			{PaneID: "w1:p2", Title: "Refactor the parser", Workspace: "api", Agent: "claude"},
		},
		Workspaces: []attachproto.WorkspaceSummary{{ID: "w1", Name: "api"}},
	}
	m.openPicker()

	if len(m.pickerItems) != 3 {
		t.Fatalf("built %d items, want two agents and a workspace", len(m.pickerItems))
	}

	m.pickerQuery = "parser"
	matches := m.pickerMatches()
	if len(matches) == 0 || matches[0].id != "w1:p2" {
		t.Fatalf("first match = %+v, want the parser agent", matches)
	}

	m.pickerQuery = "nothinglikethis"
	if got := m.pickerMatches(); len(got) != 0 {
		t.Errorf("got %d matches for a nonsense query, want none", len(got))
	}
}
