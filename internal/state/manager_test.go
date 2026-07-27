package state

import "testing"

// TestLastReplyLine uses screens captured from a real Claude Code pane. The
// composer is the trap: it holds what was *typed*, so returning it would echo
// your own question back as the manager's answer.
func TestLastReplyLine(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{
			name: "marked reply above the composer wins",
			lines: []string{
				"⏺ Three panes: a shell, the manager, and lazygit.",
				"───────────────────────────────────────────",
				"❯ how many panes exist right now?",
				"───────────────────────────────────────────",
				"  ⚠ Transcript saving is off — inherited marker · res…",
				"  multiplexer(main)  ctx:4%  5h:[██░░░░░░]31% 😎  | Opus 5",
				"  ⏵⏵ auto mode on (shift+tab to cycle)",
			},
			want: "Three panes: a shell, the manager, and lazygit.",
		},
		{
			// The exact shape that returned "❯" before: nothing but chrome and
			// an empty composer.
			name: "an empty composer is not a reply",
			lines: []string{
				"───────────────────────────────────────────",
				"❯ ",
				"───────────────────────────────────────────",
				"  multiplexer(main)  ctx:--  | Opus 5",
				"  ⏵⏵ auto mode on (shift+tab to cycle)",
			},
			want: "",
		},
		{
			name: "the newest marked line wins over an older one",
			lines: []string{
				"⏺ first answer",
				"⏺ second answer",
				"❯ ",
			},
			want: "second answer",
		},
		{
			name: "unmarked prose is used when nothing is marked",
			lines: []string{
				"ready",
				"───────────────────────────────────────────",
				"❯ ",
				"  ⏵⏵ auto mode on (shift+tab to cycle)",
			},
			want: "ready",
		},
		{
			name:  "a screen of pure chrome yields nothing",
			lines: []string{"╭────────╮", "│        │", "╰────────╯", "  ctx:4%  | Opus 5"},
			want:  "",
		},
		{
			// Captured verbatim from a live manager pane. Two traps in one
			// screen: the timing footer sits *below* the answer, and the
			// answer itself wraps onto a continuation line.
			name: "real screen: timing footer loses, wrapped answer is joined",
			lines: []string{
				"⏺ Manager ready. No task given — what you want?",
				"✻ Churned for 6s",
				"❯ answer in one short line: how many panes are open right now?",
				"⏺ I'll check with rook pane ls.",
				"  Listed 1 directory, ran 2 shell commands",
				"⏺ 1 pane in pane ls (w1:p1) — my own pane w1:p2 isn't listed, so 2 exist",
				"  counting me.",
				"✻ Crunched for 21s",
				"───────────────────────────────────────────",
				"❯ why isn't my own pane showing in pane ls?",
				"───────────────────────────────────────────",
				"  multiplexer(main)  ctx:4%  | Opus 5",
				"  ⏵⏵ auto mode on (shift+tab to cycle)",
			},
			want: "1 pane in pane ls (w1:p1) — my own pane w1:p2 isn't listed, so 2 exist counting me.",
		},
		{
			name:  "empty input",
			lines: nil,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lastReplyLine(tt.lines); got != tt.want {
				t.Errorf("lastReplyLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsChrome(t *testing.T) {
	chrome := []string{
		"────────────────",
		"╭──────────────╮",
		"  ⏵⏵ auto mode on (shift+tab to cycle)",
		"  multiplexer(main)  ctx:4%  | Opus 5",
		"✳ Cogitating… (12s · esc to interrupt)",
		"? for shortcuts",
	}
	for _, line := range chrome {
		if !isChrome(line) {
			t.Errorf("isChrome(%q) = false, want true", line)
		}
	}
	for _, line := range []string{
		"✻ Churned for 6s", "Crunched for 3m 4s", "✻ Cooked for 12s",
	} {
		if !isChrome(line) {
			t.Errorf("timing footer %q should be chrome", line)
		}
	}
	for _, line := range []string{"Three panes are open.", "ready", "done: tests pass"} {
		if isChrome(line) {
			t.Errorf("isChrome(%q) = true, want false", line)
		}
	}
}
