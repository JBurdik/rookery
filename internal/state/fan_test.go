package state

import (
	"slices"
	"testing"

	"github.com/jirkab/rookery/internal/config"
)

func TestResolveAgent(t *testing.T) {
	def := config.Agent{Command: "codex", Args: []string{"--full-auto"}}

	tests := []struct {
		name     string
		def      config.Agent
		cmd      string
		args     []string
		wantCmd  string
		wantArgs []string
	}{
		{"configured default", def, "", nil, "codex", []string{"--full-auto"}},
		{"blank cmd is no cmd", def, "  ", nil, "codex", []string{"--full-auto"}},
		// --cmd wins whole, so codex's flags never reach claude.
		{"explicit cmd drops default args", def, "claude", nil, "claude", nil},
		{"explicit cmd keeps its own args", def, "claude", []string{"-p"}, "claude", []string{"-p"}},
		{"args alone override", def, "", []string{"-p"}, "codex", []string{"-p"}},
		{"unconfigured falls back", config.Agent{Command: config.DefaultAgentCommand}, "", nil, "claude", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, args := resolveAgent(tc.def, tc.cmd, tc.args)
			if cmd != tc.wantCmd || !slices.Equal(args, tc.wantArgs) {
				t.Errorf("resolveAgent = %q %v, want %q %v", cmd, args, tc.wantCmd, tc.wantArgs)
			}
		})
	}
}

// A daemon that was never handed a config still has to fan out.
func TestNewLoopHasDefaultAgent(t *testing.T) {
	l := NewLoop("test", "dev")
	if l.agentCmd.Command != config.DefaultAgentCommand {
		t.Errorf("agentCmd = %q, want %q", l.agentCmd.Command, config.DefaultAgentCommand)
	}
	l.SetDefaultAgent(config.Agent{})
	if l.agentCmd.Command != config.DefaultAgentCommand {
		t.Error("an empty config wiped the default agent")
	}
	l.SetDefaultAgent(config.Agent{Command: "codex"})
	if l.agentCmd.Command != "codex" {
		t.Errorf("agentCmd = %q, want codex", l.agentCmd.Command)
	}
}
