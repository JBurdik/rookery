package cli

import (
	"fmt"
	"os"

	"github.com/jirkab/rookery/internal/agentstatus"
	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/config"
)

// RunAgents implements `rook agents` — inspect the status manifests, and copy
// the built-in ones out so they can be edited.
func RunAgents(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}

	switch sub {
	case "", "ls", "list":
		return agentsList()
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("usage: rook agents show <agent>")
		}
		return agentsShow(args[1])
	case "explain":
		return agentsExplain(args[1:])
	case "init":
		written, err := agentstatus.WriteDefaults(config.AgentsDir())
		if err != nil {
			return err
		}
		if len(written) == 0 {
			fmt.Printf("%s already populated; nothing overwritten\n", config.AgentsDir())
			return nil
		}
		fmt.Printf("wrote %d manifests to %s\n", len(written), config.AgentsDir())
		fmt.Println("edit any of them and restart the daemon (`rook kill`) to pick up changes")
		return nil
	case "-h", "--help", "help":
		agentsUsage()
		return nil
	default:
		agentsUsage()
		return fmt.Errorf("agents: unknown subcommand %q", sub)
	}
}

func agentsList() error {
	registry, errs := agentstatus.Load(config.AgentsDir())
	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}

	type row struct {
		Agent string   `json:"agent"`
		Exec  []string `json:"exec,omitempty"`
		Rules int      `json:"rules"`
		About string   `json:"description,omitempty"`
	}
	var out []row
	for _, id := range registry.Agents() {
		m := registry.Manifest(id)
		out = append(out, row{Agent: id, Exec: m.Exec, Rules: len(m.Rules), About: m.Description})
	}
	return printJSON(map[string]any{
		"dir":    config.AgentsDir(),
		"agents": out,
	})
}

func agentsShow(id string) error {
	registry, _ := agentstatus.Load(config.AgentsDir())
	m := registry.Manifest(id)
	if m == nil {
		return fmt.Errorf("no manifest for %q (try `rook agents ls`)", id)
	}
	return printJSON(m)
}

// agentsExplain asks the daemon that owns the pane, rather than loading local
// manifests again: its reply reflects the active registry and the exact live
// screen that produced the status.
func agentsExplain(args []string) error {
	fs := newPaneFlags("agents explain")
	if err := fs.parse(args); err != nil {
		return err
	}
	paneID, err := fs.firstArg("agents explain")
	if err != nil {
		return err
	}
	return callAndPrint(fs.session, "debug.pane", apiproto.PaneStatusParams{PaneID: paneID})
}

func agentsUsage() {
	fmt.Fprint(os.Stderr, `rook agents — the rules that decide what an agent is doing

Usage:
  rook agents ls            list known agents, their executables and rule counts
  rook agents show <agent>  print one manifest, rules and all
  rook agents explain <pane> [--session NAME]
                          show the live rule and signal behind a pane's status
  rook agents init          copy the built-in manifests into ~/.rook/agents

Status comes from prioritised rules matched against two things a multiplexer
can see: the terminal title the agent sets, and the bottom lines of its screen.
Highest priority wins; an agent's own rules are considered alongside the shared
"generic" ones.

A file in ~/.rook/agents with the same "id" as a built-in replaces it, so you
can correct a rule instead of fighting it. Rule fields:

  state     working | blocked | idle
  priority  higher wins; blocked markers sit above working ones
  region    "title" or "bottom"
  regex     Go regexp, matched against that region
  contains  all of these substrings must be present (case-insensitive)
  any       at least one of these must be present

The daemon reads them at startup, so restart it (`+"`rook kill`"+`) after editing.
`)
}
