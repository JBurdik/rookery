package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	sessionRefStdin := fs.set.String("session-ref-stdin", "",
		"read the session id from this JSON field on stdin, instead of --session-ref (for hooks that pipe their event as JSON)")
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

	ref := *sessionRef
	if *sessionRefStdin != "" {
		var payload map[string]any
		if data, err := io.ReadAll(os.Stdin); err == nil {
			_ = json.Unmarshal(data, &payload)
		}
		if v, _ := payload[*sessionRefStdin].(string); v != "" {
			ref = v
		} else {
			// The event this hook fired for didn't carry the field we wanted —
			// nothing to report, and a hook must not make noise over that.
			return nil
		}
	}

	// A caller who never touched --status is reporting a session id only
	// (Codex's SessionStart, OpenCode's session.updated) and must not clear
	// the pane's lifecycle status back to screen detection as a side effect.
	statusGiven := false
	fs.set.Visit(func(f *flag.Flag) {
		if f.Name == "status" {
			statusGiven = true
		}
	})

	resp, err := apiCall(fs.session, apiproto.Request{
		ID:     "report",
		Method: "pane.report",
		Params: mustJSON(apiproto.PaneReportParams{
			PaneID:     pane,
			Status:     *status,
			SessionRef: ref,
			Agent:      *agent,
			KeepStatus: !statusGiven,
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
	fs := newPaneFlags("integration status")
	var target targetFlags
	target.register(fs.set)
	if err := fs.parse(args); err != nil {
		return err
	}

	type row struct {
		integration.Status
		ConfigDir string `json:"config_dir"`
		Active    bool   `json:"active"`
		Available bool   `json:"available"`
	}
	var out []row

	for _, id := range integration.IDs() {
		spec := integration.Specs[id]
		_, lookErr := exec.LookPath(id)

		// Report every configuration, not just the active one: with several
		// live configs the useful question is *which* of them has it.
		paths, err := target.resolve(spec)
		if err != nil {
			return err
		}
		active := ""
		if len(paths) > 0 {
			active = paths[0]
		}

		dirs := candidates(spec)
		pathsForDir := map[string]string{}
		if target.settings != "" || target.configDir != "" || target.project || target.local {
			dirs = nil // an explicit target means only that one matters
			for _, p := range paths {
				dir := filepath.Dir(p)
				dirs = append(dirs, dir)
				pathsForDir[dir] = p
			}
		}
		for _, dir := range dirs {
			path := pathsForDir[dir]
			if path == "" {
				path = spec.SettingsIn(dir)
			}
			st, err := integration.StatusOf(id, path)
			if err != nil {
				return err
			}
			out = append(out, row{
				Status:    st,
				ConfigDir: dir,
				Active:    path == active,
				Available: lookErr == nil,
			})
		}
	}
	return printJSON(map[string]any{
		"integrations": out,
		"note": "active is the config the agent itself would load; " +
			"target another with --config-dir, --project, --local or --settings",
	})
}

func integrationChange(args []string, install bool) error {
	fs := newPaneFlags("integration install")
	var target targetFlags
	target.register(fs.set)
	if err := fs.parse(args); err != nil {
		return err
	}
	ids := fs.args()
	if len(ids) == 0 {
		return fmt.Errorf("usage: rook integration %s <%s> [--config-dir DIR]",
			map[bool]string{true: "install", false: "uninstall"}[install],
			strings.Join(integration.IDs(), "|"))
	}

	// The absolute path matters: hooks run with whatever environment the agent
	// has, and "rook" may not be on it.
	bin := "rook"
	if exe, err := os.Executable(); err == nil {
		bin = exe
	}

	for _, id := range ids {
		spec, known := integration.Specs[id]
		if !known {
			return fmt.Errorf("unknown integration %q (have: %s)", id, strings.Join(integration.IDs(), ", "))
		}
		paths, err := target.resolve(spec)
		if err != nil {
			return err
		}
		for _, path := range paths {
			var st integration.Status
			if install {
				st, err = integration.Install(id, path, bin)
			} else {
				st, err = integration.Uninstall(id, path)
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
		if install && spec.ConfigEnv != "" && os.Getenv(spec.ConfigEnv) != "" {
			fmt.Printf("  (%s is set, so that is the config used; --all covers the others)\n", spec.ConfigEnv)
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
  rook integration status              what is installed, in every config found
  rook integration install claude      add the integration
  rook integration uninstall claude    remove it

Which configuration:
  (default)              the one the agent itself would load — $CLAUDE_CONFIG_DIR
                         if set, otherwise ~/.claude
  --config-dir DIR       a specific config directory, e.g. ~/claude-personal
  --project              ./.claude/settings.json, shared with the repo
  --local                ./.claude/settings.local.json, gitignored
  --settings FILE        an exact file
  --all                  every config directory found

Several live configurations is the normal case, and writing hooks into one the
agent never loads is the worst outcome — it reports success and changes nothing.
`+"`status`"+` therefore lists every config it can find and marks the active one.

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

Pi uses its native TypeScript extension API instead of JSON hooks. Installing
Pi writes one auto-discovered extension at ~/.pi/agent/extensions/rook-agent-state.ts
(or ./.pi/extensions with --project); it reports working/idle and its session
reference, and leaves Pi's own settings untouched.
`)
}
