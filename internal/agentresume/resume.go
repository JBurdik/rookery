// Package agentresume contains the deliberately small set of commands Rookery
// may use to reopen an agent's own recorded session. Keeping this mapping out
// of the CLI and daemon means there is one auditable answer to "what will
// resume run?" and no user supplied command line is ever interpreted here.
package agentresume

import (
	"fmt"
	"strings"
)

// Command is the exact program and arguments used for a resume. Args never
// contains shell syntax: PTYs execute this command directly.
type Command struct {
	Cmd  string
	Args []string
}

// CommandFor returns the documented interactive resume invocation for a
// supported agent. It intentionally has no general fallback; adding another
// agent means making its invocation explicit and testing it here.
func CommandFor(agent, sessionRef string) (Command, error) {
	if strings.TrimSpace(sessionRef) == "" {
		return Command{}, fmt.Errorf("session_ref is required")
	}
	switch agent {
	case "claude":
		return Command{Cmd: "claude", Args: []string{"--resume", sessionRef}}, nil
	case "codex":
		return Command{Cmd: "codex", Args: []string{"resume", sessionRef}}, nil
	case "opencode":
		return Command{Cmd: "opencode", Args: []string{"--session", sessionRef}}, nil
	default:
		return Command{}, fmt.Errorf("unsupported agent %q (supported: claude, codex, opencode)", agent)
	}
}
