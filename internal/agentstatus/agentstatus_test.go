package agentstatus

import "testing"

// reg is the built-in manifest set, which is what ships and therefore what
// these tests should be checking.
func reg(t *testing.T) *Registry {
	t.Helper()
	r, errs := Load("")
	for _, err := range errs {
		t.Fatalf("loading built-in manifests: %v", err)
	}
	return r
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name string
		in   Input
		want State
	}{
		{
			name: "braille spinner in the title means working",
			in:   Input{Title: "⠹ claude", Bottom: []string{"› "}},
			want: Working,
		},
		{
			name: "interrupt hint means working",
			in:   Input{Bottom: []string{"✳ Cogitating… (12s · esc to interrupt)"}},
			want: Working,
		},
		{
			name: "yes/no dialog means blocked",
			in:   Input{Bottom: []string{"Do you want to make this edit to main.go?", "❯ 1. Yes", "  2. No, tell Claude what to do"}},
			want: Blocked,
		},
		{
			name: "menu navigation footer means blocked",
			in:   Input{Bottom: []string{"Select a model", "↑/↓ to navigate · enter to select · esc to cancel"}},
			want: Blocked,
		},
		{
			name: "blocked outranks a still-spinning title",
			in:   Input{Title: "⠹ working", Bottom: []string{"Allow this command to run? (y/n)"}},
			want: Blocked,
		},
		{
			name: "shortcut hint at the prompt means idle",
			in:   Input{Title: "claude", Bottom: []string{"│ > ", "? for shortcuts"}},
			want: Idle,
		},
		{
			name: "elapsed counter means working",
			in:   Input{Bottom: []string{"✳ Cogitating… (12s)"}},
			want: Working,
		},
		{
			// Regression: a bare "thinking" substring rule matched the word
			// in finished output and left the pane stuck on "working".
			name: "finished output mentioning thinking is not working",
			in:   Input{Title: "claude", Bottom: []string{"done thinking", "  ? for shortcuts"}},
			want: Idle,
		},
		{
			name: "a plain shell matches nothing",
			in:   Input{Title: "bash", Bottom: []string{"$ "}},
			want: Unknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reg(t).Evaluate("claude", tt.in); got != tt.want {
				t.Errorf("Evaluate(%+v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestEvaluateAgentRealClaude uses screens captured from Claude Code 2.1
// running in a rookery pane. The point of the fixture is the bottom lines:
// they are identical whether it is working or idle, so anything that infers
// state from screen churn gets it wrong. Only the title differs.
func TestEvaluateAgentRealClaude(t *testing.T) {
	bottom := []string{
		"────────────────────────────────────────",
		"❯ ",
		"────────────────────────────────────────",
		"  ⚠ Transcript saving is off — inherite…",
		"  multiplexer  ctx:4%  5h:[█░░░░░░░]14…",
		"  ⏵⏵ auto mode on (shift+tab to cycle)",
	}

	if got := reg(t).EvaluateAgent("claude", Input{Title: "⠐ Count from 1 to 30", Bottom: bottom}); got != Working {
		t.Errorf("spinner title = %q, want working", got)
	}
	if got := reg(t).EvaluateAgent("claude", Input{Title: "✳ Claude Code", Bottom: bottom}); got != Idle {
		t.Errorf("settled title = %q, want idle", got)
	}
	if got := reg(t).EvaluateAgent("claude", Input{Title: "✳ Count from 1 to 30", Bottom: bottom}); got != Idle {
		t.Errorf("settled title after a turn = %q, want idle", got)
	}

	// A recognised agent never reports unknown: no marker means "at its
	// prompt", which is what stops the caller reaching for output activity.
	if got := reg(t).EvaluateAgent("claude", Input{Title: "claude", Bottom: []string{"$"}}); got != Idle {
		t.Errorf("unmarked agent screen = %q, want idle", got)
	}
	// …but a plain Evaluate still says so, which is how non-agent panes get
	// routed to the foreground-process check instead.
	if got := reg(t).Evaluate("", Input{Title: "zsh", Bottom: []string{"$"}}); got != Unknown {
		t.Errorf("Evaluate on a shell = %q, want unknown", got)
	}
}

func TestAgent(t *testing.T) {
	tests := []struct {
		cmd  string
		args []string
		want string
	}{
		{cmd: "/opt/homebrew/bin/claude", want: "claude"},
		{cmd: "claude", want: "claude"},
		{cmd: "CODEX", want: "codex"},
		{cmd: "/usr/bin/npx", args: []string{"opencode"}, want: "opencode"},
		{cmd: "/bin/bash", want: ""},
		{cmd: "/bin/zsh", args: []string{"-lc", "npm test"}, want: ""},
	}
	for _, tt := range tests {
		if got := reg(t).Agent(tt.cmd, tt.args); got != tt.want {
			t.Errorf("Agent(%q, %v) = %q, want %q", tt.cmd, tt.args, got, tt.want)
		}
	}
}

func TestCleanTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		// Real Claude Code titles: spinner frame, then the task.
		{"⠐ Count from 1 to 30", "Count from 1 to 30"},
		{"✳ Claude Code", "Claude Code"},
		{"✳ Count from 1 to 30", "Count from 1 to 30"},
		{"  ⠹   spaced   out  ", "spaced out"},
		// Only a spinner: nothing worth showing.
		{"⠹", ""},
		{"", ""},
		// The launching shell still owns the title during startup and sets it
		// to the command line or cwd; neither is a pane name.
		{"/Users/jirkab/.local/bin/claude", ""},
		{"..n/multiplexer", ""},
		{"-zsh", ""},
		// A plain title passes through.
		{"npm test", "npm test"},
	}
	for _, tt := range tests {
		if got := CleanTitle(tt.in); got != tt.want {
			t.Errorf("CleanTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
