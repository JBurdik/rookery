package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	remote, name, err := attachArgs(args)
	if err != nil {
		return err
	}
	if remote != "" {
		return runRemoteAttach(remote, name)
	}
	if err := EnsureDaemon(name); err != nil {
		return err
	}
	if err := ensureDefaultPane(name); err != nil {
		return err
	}
	return tui.Run(name, Version)
}

// attachArgs accepts a session name plus --remote HOST. Remote attach is
// intentionally transport-simple: ssh owns authentication, host aliases and
// reconnecting, while the remote rook binary owns its local Unix sockets.
// That avoids exposing a control socket to the network just to attach a TUI.
func attachArgs(args []string) (remote, name string, err error) {
	name = defaultSessionName()
	hasName := false
	for len(args) > 0 {
		switch args[0] {
		case "--remote", "-r":
			if remote != "" || len(args) < 2 || strings.HasPrefix(args[1], "-") {
				return "", "", errors.New("usage: rook attach [session] [--remote user@host]")
			}
			remote = args[1]
			args = args[2:]
		default:
			if strings.HasPrefix(args[0], "-") || hasName {
				return "", "", fmt.Errorf("usage: rook attach [session] [--remote user@host]")
			}
			name = args[0]
			hasName = true
			args = args[1:]
		}
	}
	return remote, name, nil
}

// runRemoteAttach replaces this process with an interactive SSH session. The
// remote command is quoted explicitly because ssh executes it through the
// remote shell; a session name must never become shell syntax there.
func runRemoteAttach(target, name string) error {
	cmd := exec.Command("ssh", "-tt", target, "rook attach "+shellQuote(name))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
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
