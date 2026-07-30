package cli

import (
	"slices"
	"strings"
	"testing"
)

// TestPaneFlagsParse covers the argument permutation: flags must work on
// either side of positional arguments, and everything after `--` must reach
// the wrapped command untouched.
func TestPaneFlagsParse(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    []string
		wantRaw bool
		session string
	}{
		{name: "flag before positional", args: []string{"--raw", "p1"}, want: []string{"p1"}, wantRaw: true, session: "default"},
		{name: "flag after positional", args: []string{"p1", "--raw"}, want: []string{"p1"}, wantRaw: true, session: "default"},
		{name: "no flags", args: []string{"p1"}, want: []string{"p1"}, session: "default"},
		{name: "session flag", args: []string{"p1", "--session", "work"}, want: []string{"p1"}, session: "work"},
		{name: "words after positional", args: []string{"p1", "fix", "the", "test"}, want: []string{"p1", "fix", "the", "test"}, session: "default"},
		{
			name:    "double dash protects wrapped command flags",
			args:    []string{"--raw", "--", "claude", "--raw", "-p"},
			want:    []string{"claude", "--raw", "-p"},
			wantRaw: true,
			session: "default",
		},
	}

	t.Setenv(SessionEnvVar, "") // don't inherit the developer's own session

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newPaneFlags("test")
			raw := fs.set.Bool("raw", false, "")
			if err := fs.parse(tt.args); err != nil {
				t.Fatalf("parse(%q): %v", tt.args, err)
			}
			if got := fs.args(); !slices.Equal(got, tt.want) {
				t.Errorf("positional args = %q, want %q", got, tt.want)
			}
			if *raw != tt.wantRaw {
				t.Errorf("--raw = %v, want %v", *raw, tt.wantRaw)
			}
			if fs.session != tt.session {
				t.Errorf("session = %q, want %q", fs.session, tt.session)
			}
		})
	}
}

func TestEnvFlag(t *testing.T) {
	var e envFlag
	if err := e.Set("FOO=bar"); err != nil {
		t.Fatal(err)
	}
	if err := e.Set("EMPTY="); err != nil {
		t.Fatal(err)
	}
	if err := e.Set("nope"); err == nil {
		t.Error("Set(\"nope\") should reject a value with no '='")
	}
	if e["FOO"] != "bar" || e["EMPTY"] != "" {
		t.Errorf("envFlag = %v, want FOO=bar and EMPTY=", e)
	}
}

func TestAgentsExplainRequiresPane(t *testing.T) {
	err := RunAgents([]string{"explain"})
	if err == nil || !strings.Contains(err.Error(), "usage: rook agents explain <pane-id>") {
		t.Errorf("RunAgents(explain) error = %v, want missing-pane usage", err)
	}
}

func TestEncodeKeys(t *testing.T) {
	got := encodeKeys([]string{"hello", "Enter", "C-c", "left", "literal"})
	want := "hello\r\x03\x1b[Dliteral"
	if got != want {
		t.Errorf("encodeKeys = %q, want %q", got, want)
	}
}
