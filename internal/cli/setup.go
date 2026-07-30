package cli

import (
	"fmt"
	"os"

	"github.com/jirkab/rookery/internal/tui"
)

// RunSetup implements `rook setup` — an interactive wizard over `rook skill
// --install` and `rook integration install`, for the common case of "wire up
// whatever agents I have" without reading either command's flags first.
func RunSetup(args []string) error {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			setupUsage()
			return nil
		}
	}

	if !isTerminal(os.Stdout) {
		setupUsage()
		return fmt.Errorf("rook setup needs a terminal; use `rook skill --install` and `rook integration install <agent>` instead")
	}
	return tui.RunSetupWizard()
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

func setupUsage() {
	fmt.Fprint(os.Stderr, `rook setup — an interactive wizard to wire up agents

Usage:
  rook setup

Walks through the agents rookery knows about (Claude Code, Codex, OpenCode, Pi),
lets you pick which to set up, and installs both the rookery skill and the
status-reporting hooks for each — the same install `+"`rook skill --install`"+`
and `+"`rook integration install <agent>`"+` do individually, into whichever
config each agent would actually load.

Not a terminal (piped, scripted, CI)? Use the flag-driven commands directly:
  rook skill --install --agent <agent> [--project|--local|--config-dir DIR|--all]
  rook integration install <claude|codex|opencode|pi> [--project|--local|--config-dir DIR|--all]
`)
}
