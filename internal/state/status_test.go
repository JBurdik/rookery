package state

import "testing"

func TestIsShell(t *testing.T) {
	tests := []struct {
		running, cmd string
		want         bool
	}{
		{running: "zsh", cmd: "/bin/zsh", want: true},
		{running: "bash", cmd: "/bin/bash", want: true},
		{running: "fish", cmd: "/opt/homebrew/bin/fish", want: true},
		// A shell running a job: still a shell pane, so the foreground
		// process group is the signal to trust.
		{running: "sleep", cmd: "/bin/zsh", want: true},
		// A pane whose own program is the job.
		{running: "npm", cmd: "/usr/bin/npm", want: false},
		{running: "claude", cmd: "/Users/x/.local/bin/claude", want: false},
	}
	for _, tt := range tests {
		if got := isShell(tt.running, tt.cmd); got != tt.want {
			t.Errorf("isShell(%q, %q) = %v, want %v", tt.running, tt.cmd, got, tt.want)
		}
	}
}
