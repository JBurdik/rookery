package agentresume

import (
	"slices"
	"testing"
)

func TestCommandFor(t *testing.T) {
	tests := []struct {
		agent, cmd string
		args       []string
	}{
		{"claude", "claude", []string{"--resume", "session-1"}},
		{"codex", "codex", []string{"resume", "session-1"}},
		{"opencode", "opencode", []string{"--session", "session-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			got, err := CommandFor(tt.agent, "session-1")
			if err != nil || got.Cmd != tt.cmd || !slices.Equal(got.Args, tt.args) {
				t.Fatalf("CommandFor(%q) = %#v, %v; want %q %q", tt.agent, got, err, tt.cmd, tt.args)
			}
		})
	}
}

func TestCommandForRejectsUnknownAgentAndEmptyReference(t *testing.T) {
	if _, err := CommandFor("unknown", "session-1"); err == nil {
		t.Fatal("unknown agent was accepted")
	}
	if _, err := CommandFor("claude", " \t"); err == nil {
		t.Fatal("empty session reference was accepted")
	}
}
