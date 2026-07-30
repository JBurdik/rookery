package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jirkab/rookery/internal/apiproto"
)

// RunWait implements `rook wait <what> <pane>`: block until a pane reaches a
// state, then print what happened. This is the piece that lets one agent
// orchestrate others — spawn a pane, send it a task, wait for it to finish,
// read the result — instead of polling in a shell loop.
//
// Exit status is 0 on a match and 1 on timeout, so `rook wait … && rook pane
// read …` reads naturally in a script.
func RunWait(args []string) error {
	if len(args) == 0 {
		waitUsage()
		return errors.New("wait: missing subcommand")
	}

	switch args[0] {
	case "agent-status", "status":
		return waitAgentStatus(args[1:])
	case "exit":
		return waitExit(args[1:])
	case "output":
		return waitOutput(args[1:])
	case "-h", "--help", "help":
		waitUsage()
		return nil
	default:
		waitUsage()
		return fmt.Errorf("wait: unknown subcommand %q", args[0])
	}
}

func waitOutput(args []string) error {
	fs := newPaneFlags("wait output")
	match := fs.set.String("match", "", "literal text to wait for")
	regex := fs.set.String("regex", "", "regular expression to wait for")
	scrollback := fs.set.Bool("scrollback", false, "match retained scrollback instead of current screen")
	timeout := fs.set.Int("timeout", 0, "milliseconds before giving up (0 = 5m default, -1 = wait forever)")
	current := fs.set.Bool("current", false, "wait on the calling pane ($ROOK_PANE)")
	if err := fs.parse(args); err != nil {
		return err
	}
	paneID, err := waitTarget(fs, *current)
	if err != nil {
		return err
	}
	if (*match == "") == (*regex == "") {
		return errors.New("provide exactly one of --match or --regex")
	}
	source := "screen"
	if *scrollback {
		source = "scrollback"
	}
	params := apiproto.WaitOutputParams{PaneID: paneID, Match: *match, Regex: *regex, Source: source, TimeoutMS: *timeout}
	resp, err := apiCallTimeout(fs.session, apiproto.Request{ID: "wait-output", Method: "wait.output", Params: mustJSON(params)}, waitDeadline(*timeout))
	if err != nil {
		return err
	}
	if resp.Result != nil {
		if err := printJSON(resp.Result); err != nil {
			return err
		}
	}
	if resp.Error != nil {
		if resp.Error.Code == apiproto.ErrTimeout {
			os.Exit(1)
		}
		return respError(resp)
	}
	return nil
}

func waitAgentStatus(args []string) error {
	fs := newPaneFlags("wait agent-status")
	status := fs.set.String("status", "", "comma-separated statuses to wait for: idle,working,blocked,done")
	timeout := fs.set.Int("timeout", 0, "milliseconds before giving up (0 = 5m default, -1 = wait forever)")
	current := fs.set.Bool("current", false, "wait on the calling pane ($ROOK_PANE)")
	if err := fs.parse(args); err != nil {
		return err
	}

	paneID, err := waitTarget(fs, *current)
	if err != nil {
		return err
	}
	states := splitStates(*status)
	if len(states) == 0 {
		return errors.New("--status is required, e.g. --status done or --status idle,blocked")
	}
	for _, s := range states {
		if !validStates[s] {
			return fmt.Errorf("unknown status %q (want idle, working, blocked or done)", s)
		}
	}

	return runWaitCall(fs.session, apiproto.WaitPaneParams{
		PaneID:      paneID,
		AgentStatus: states,
		TimeoutMS:   *timeout,
	})
}

func waitExit(args []string) error {
	fs := newPaneFlags("wait exit")
	timeout := fs.set.Int("timeout", 0, "milliseconds before giving up (0 = 5m default, -1 = wait forever)")
	current := fs.set.Bool("current", false, "wait on the calling pane ($ROOK_PANE)")
	if err := fs.parse(args); err != nil {
		return err
	}
	paneID, err := waitTarget(fs, *current)
	if err != nil {
		return err
	}
	return runWaitCall(fs.session, apiproto.WaitPaneParams{PaneID: paneID, TimeoutMS: *timeout})
}

func waitTarget(fs *paneFlags, current bool) (string, error) {
	if args := fs.args(); len(args) > 0 {
		return args[0], nil
	}
	if current {
		if id := os.Getenv(PaneEnvVar); id != "" {
			return id, nil
		}
		return "", errors.New("--current used outside a rookery pane ($" + PaneEnvVar + " is unset)")
	}
	return "", errors.New("usage: rook wait <agent-status|exit> <pane-id> [flags]")
}

// runWaitCall keeps the connection open for as long as the daemon needs: the
// wait is answered by the daemon when the condition happens, not polled from
// here, so the only deadline that matters is the one the daemon enforces.
func runWaitCall(session string, params apiproto.WaitPaneParams) error {
	resp, err := apiCallTimeout(session, apiproto.Request{
		ID:     "wait",
		Method: "wait.pane",
		Params: mustJSON(params),
	}, waitDeadline(params.TimeoutMS))
	if err != nil {
		return err
	}

	// A timeout still carries a result: print it, then fail the command so a
	// shell `&&` chain stops.
	if resp.Result != nil {
		if err := printJSON(resp.Result); err != nil {
			return err
		}
	}
	if resp.Error != nil {
		if resp.Error.Code == apiproto.ErrTimeout {
			os.Exit(1)
		}
		return respError(resp)
	}
	return nil
}

// waitDeadline is the client-side socket deadline: always a little longer
// than the daemon's own timeout, so the daemon's structured "timed out"
// answer wins the race against a bare connection error.
func waitDeadline(timeoutMS int) time.Duration {
	switch {
	case timeoutMS < 0:
		return 0 // no deadline
	case timeoutMS == 0:
		return 5*time.Minute + 30*time.Second
	default:
		return time.Duration(timeoutMS)*time.Millisecond + 30*time.Second
	}
}

func splitStates(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(strings.ToLower(part)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var validStates = map[string]bool{"idle": true, "working": true, "blocked": true, "done": true}

func waitUsage() {
	fmt.Fprint(os.Stderr, `rook wait — block until a pane reaches a state

Usage:
  rook wait agent-status <pane> --status done [--timeout MS]
  rook wait exit <pane> [--timeout MS]
  rook wait output <pane> --match TEXT [--scrollback] [--timeout MS]
  rook wait output <pane> --regex PATTERN [--timeout MS]

Statuses:
  working   the agent is running a turn
  blocked   the agent is asking for input or confirmation
  idle      the agent is at its prompt, and you have seen the result
  done      the agent is at its prompt and you have not seen the result yet

A pane already in a wanted state matches immediately. Exit status is 0 on a
match and 1 on timeout, so this chains:

  rook pane send p2 run the tests
  rook wait agent-status p2 --status done,blocked --timeout 120000 \
    && rook pane read p2 --scrollback --lines 60 --raw
`)
}
