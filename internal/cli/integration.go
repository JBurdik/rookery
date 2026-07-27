package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/integration"
)

// RunReport implements `rook report` — an agent integration telling rookery
// what it is doing.
//
// Called from an agent's hooks, so it has two hard requirements: be fast, and
// never make noise. A hook that prints or fails is a hook that shows up in the
// middle of somebody's turn.
func RunReport(args []string) error {
	fs := newPaneFlags("report")
	status := fs.set.String("status", "", "idle | working | blocked (empty hands the pane back to screen detection)")
	sessionRef := fs.set.String("session-ref", "", "the agent's own session id, kept for a future resume")
	agent := fs.set.String("agent", "", "which agent is reporting")
	quiet := fs.set.Bool("quiet", false, "print nothing, and exit 0 even on failure")
	if err := fs.parse(args); err != nil {
		if *quiet {
			return nil
		}
		return err
	}

	pane := os.Getenv(PaneEnvVar)
	if rest := fs.args(); len(rest) > 0 {
		pane = rest[0]
	}
	if pane == "" {
		if *quiet {
			return nil
		}
		return errors.New("not inside a rookery pane ($" + PaneEnvVar + " is unset)")
	}

	resp, err := apiCall(fs.session, apiproto.Request{
		ID:     "report",
		Method: "pane.report",
		Params: mustJSON(apiproto.PaneReportParams{
			PaneID:     pane,
			Status:     *status,
			SessionRef: *sessionRef,
			Agent:      *agent,
		}),
	})
	if *quiet {
		// Whatever happened, a hook must not fail. Rookery not running is the
		// normal case here — the agent is simply not in a pane today.
		return nil
	}
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return respError(resp)
	}
	return printJSON(resp.Result)
}

// RunIntegration implements `rook integration` — install the agent-side hooks
// that make status authoritative instead of inferred.
func RunIntegration(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "status", "ls", "list":
		return integrationStatus(args)
	case "install":
		return integrationChange(args[1:], true)
	case "uninstall", "remove":
		return integrationChange(args[1:], false)
	case "-h", "--help", "help":
		integrationUsage()
		return nil
	default:
		integrationUsage()
		return fmt.Errorf("integration: unknown subcommand %q", sub)
	}
}

func integrationStatus(args []string) error {
	_ = args
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	type row struct {
		integration.Status
		Available bool `json:"available"`
	}
	var out []row
	for _, id := range integration.IDs() {
		st, err := integration.StatusOf(id, home)
		if err != nil {
			return err
		}
		_, lookErr := exec.LookPath(id)
		out = append(out, row{Status: st, Available: lookErr == nil})
	}
	return printJSON(map[string]any{"integrations": out})
}

func integrationChange(args []string, install bool) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: rook integration %s <%s>",
			map[bool]string{true: "install", false: "uninstall"}[install],
			strings.Join(integration.IDs(), "|"))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	// The absolute path matters: hooks run with whatever environment the agent
	// has, and "rook" may not be on it.
	bin := "rook"
	if exe, err := os.Executable(); err == nil {
		bin = exe
	}

	for _, id := range args {
		if strings.HasPrefix(id, "-") {
			continue
		}
		var st integration.Status
		if install {
			st, err = integration.Install(id, home, bin)
		} else {
			st, err = integration.Uninstall(id, home)
		}
		if err != nil {
			return err
		}
		verb := "installed"
		if !install {
			verb = "removed"
		}
		fmt.Printf("%s %s (%d hooks) in %s\n", verb, st.Name, st.Hooks, st.Settings)
		if st.Note != "" {
			fmt.Printf("  note: %s\n", st.Note)
		}
	}
	if install {
		fmt.Println("\nrestart the agent for its hooks to load.")
	}
	return nil
}

func integrationUsage() {
	fmt.Fprint(os.Stderr, `rook integration — let agents report their own status

Usage:
  rook integration status              what is installed, and which agents are on PATH
  rook integration install claude      add the hooks
  rook integration uninstall claude    remove them

Without an integration, rookery works out what an agent is doing by reading its
terminal title and the bottom of its screen. That is a good heuristic and it is
what makes an unknown agent work at all, but it is still a guess.

With one installed, the agent says so itself. For Claude Code:

  UserPromptSubmit  → working     a turn started
  Stop, StopFailure → idle        the turn ended
  PermissionRequest → blocked     it is waiting on you to allow something
  Elicitation       → blocked     an MCP server wants input
  Notification      → blocked     Claude Code raised a notification
  SessionStart      → idle        it is up and waiting

A fresh report wins over screen detection for that pane. If the reports stop —
an agent killed mid-turn, say — rookery falls back to reading the screen rather
than leaving the pane stuck.

The installer merges into your existing settings.json: your own hooks are kept,
running it twice does not duplicate anything, and uninstall removes only the
entries rookery added. A settings file it cannot parse is refused rather than
replaced.
`)
}
