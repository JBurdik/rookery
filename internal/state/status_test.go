package state

import (
	"testing"
	"time"
)

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

func TestBlinkPhaseAlternates(t *testing.T) {
	base := time.UnixMilli(0)
	if !blinkPhase(base) {
		t.Error("phase at t=0 should be on")
	}
	if blinkPhase(base.Add(blinkInterval)) {
		t.Error("phase should flip after one interval")
	}
	if !blinkPhase(base.Add(2 * blinkInterval)) {
		t.Error("phase should flip back after two")
	}
	// Both sides derive it from the clock, so the same instant must agree.
	at := time.UnixMilli(1_700_000_123_456)
	if blinkPhase(at) != blinkPhase(at) {
		t.Error("blinkPhase is not deterministic")
	}
}
