package cli

import (
	"strings"

	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/tui"
)

// RunAttach implements `rook attach [session]`. If no daemon is running for
// the session yet, it starts one first (tmux `new-session -A` ergonomics).
// If the session has no panes yet, it spawns a default shell pane first —
// otherwise attach lands on a blank, keyboard-dead screen, which looks
// broken even though it's technically "working as designed".
func RunAttach(args []string) error {
	name := sessionArg(args)
	if err := EnsureDaemon(name); err != nil {
		return err
	}
	if err := ensureDefaultPane(name); err != nil {
		return err
	}
	return tui.Run(name, Version)
}

func ensureDefaultPane(name string) error {
	resp, err := apiCall(name, apiproto.Request{ID: "attach-list", Method: "pane.list"})
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return nil // let the TUI surface the connection problem
	}
	var result apiproto.PaneListResult
	if err := decodeResult(resp.Result, &result); err != nil || len(result.Panes) > 0 {
		return nil
	}

	// Empty cmd means the daemon's default shell, and deliberately no label:
	// a label is a manual override that outranks detection, so hard-coding
	// "shell" here would leave the pane called "shell" forever, even after
	// you start an agent in it.
	if _, err := apiCall(name, apiproto.Request{ID: "attach-create", Method: "pane.create",
		Params: mustJSON(apiproto.PaneCreateParams{})}); err != nil {
		return err
	}
	return nil
}

// sessionArg reads an optional positional session name from a subcommand's
// args (rook's subcommands take at most one flag-free positional argument).
func sessionArg(args []string) string {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0]
	}
	return defaultSessionName()
}
