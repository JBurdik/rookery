// Command mux is a terminal multiplexer for orchestrating coding agents: a
// detachable daemon holds PTYs/agents alive, a Bubble Tea client attaches
// and detaches at will, and agents drive the whole thing over a JSON-RPC
// Unix socket.
package main

import (
	"fmt"
	"os"

	"github.com/jirkab/rookery/internal/cli"
)

func main() {
	if len(os.Args) < 2 {
		// Bare `rook`: attach to the default session, auto-starting a
		// daemon if none is running yet — same ergonomics as Herdr's
		// (and tmux's) bare invocation.
		if err := cli.RunAttach(nil); err != nil {
			fmt.Fprintln(os.Stderr, "rook:", err)
			os.Exit(1)
		}
		return
	}

	var err error
	switch os.Args[1] {
	case "serve":
		err = cli.RunServe(os.Args[2:])
	case "attach":
		err = cli.RunAttach(os.Args[2:])
	case "--remote":
		// Keep the pleasant `rook --remote host` spelling alongside the
		// explicit `rook attach --remote host` form.
		err = cli.RunAttach(os.Args[1:])
	case "ls":
		err = cli.RunLs(os.Args[2:])
	case "status":
		err = cli.RunStatus(os.Args[2:])
	case "reload":
		err = cli.RunReload(os.Args[2:])
	case "delete", "rm":
		err = cli.RunDelete(os.Args[2:])
	case "session":
		err = cli.RunSession(os.Args[2:])
	case "pane":
		err = cli.RunPane(os.Args[2:])
	case "workspace", "ws":
		err = cli.RunWorkspace(os.Args[2:])
	case "tab":
		err = cli.RunTab(os.Args[2:])
	case "api":
		err = cli.RunAPI(os.Args[2:])
	case "wait":
		err = cli.RunWait(os.Args[2:])
	case "report":
		err = cli.RunReport(os.Args[2:])
	case "integration":
		err = cli.RunIntegration(os.Args[2:])
	case "skill":
		err = cli.RunSkill(os.Args[2:])
	case "setup":
		err = cli.RunSetup(os.Args[2:])
	case "fan":
		err = cli.RunFan(os.Args[2:])
	case "watch":
		err = cli.RunWatch(os.Args[2:])
	case "git":
		err = cli.RunGit(os.Args[2:])
	case "agents":
		err = cli.RunAgents(os.Args[2:])
	case "kill":
		err = cli.RunKill(os.Args[2:])
	case "ping":
		err = cli.RunPing(os.Args[2:])
	case "completion":
		err = cli.RunCompletion(os.Args[2:])
	case "version", "--version", "-v":
		// Worth having as more than a footnote in the help text: the client
		// already refuses to trust a daemon built from a different version, so
		// "which one am I running" is a question that actually comes up.
		fmt.Println("rook", cli.Version)
	case "-h", "--help", "help":
		usage()
		return
	default:
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "rook:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `rookery — a terminal multiplexer for coding agents

Humans:
  rook                                            attach to the default session (auto-starts it)
  rook serve [--session NAME] [--foreground|-f]   start (or run) the daemon
  rook attach [session]                           attach a TUI to a session
  rook attach [session] --remote user@host        attach through SSH to a remote session
  rook ls                                         list sessions
  rook status [session]                           show a session's workload and agent states
  rook kill [session]                             stop a session's daemon
  rook delete [session]                           permanently delete a stopped session
  rook reload [session]                           reload daemon config and agent manifests
  rook ping [session]                             check daemon liveness
  rook session ls|attach|status|kill|delete       namespaced aliases for session lifecycle

Agents / scripts — JSON output, see `+"`rook pane help`"+` and `+"`rook wait help`"+`:
  rook workspace ls|new|focus|rename|close        one workspace per repo or task
  rook tab ls|new|focus|rename|close              a layout of panes in a workspace
  rook pane ls                                    list panes, with agent status
  rook pane new [--label L] [-- cmd args...]      spawn a pane (splits the focused one)
  rook pane send <pane> [text...]                 type into a pane, then Enter
  rook pane read <pane> [--scrollback] [--raw]    read a pane's output
  rook pane focus|status|rename|kill <pane>       switch to / inspect / label / stop a pane
  rook wait agent-status <pane> --status done     block until an agent finishes
  rook wait exit <pane>                           block until a pane's process ends
  rook fan "<task>" [--agents N]                  one prompt, N agents, one worktree each
  rook fan ls|review|promote|clean                compare, retain, or tidy up
  rook watch [--status done,blocked]              stream agent state changes as NDJSON
  rook git                                        open a git UI (lazygit/gitui/tig) in a pane
  rook integration install claude                 let the agent report its own status
  rook skill --install                            teach agents to drive rookery
  rook setup                                      interactive wizard for both, per agent
  rook agents ls|show|explain|init                inspect status rules or explain a pane verdict

Shell completion:
  rook completion bash|zsh                         print a completion script (source it in your shell)
  rook api <method> ['{json params}']             call any API method directly

Agent status: working · blocked (wants input) · done (finished, unseen) ·
idle (finished, seen) · unknown.

In the TUI, ctrl+b then:
  c new tab      v split right   - split down   z zoom       x close pane
  h/j/k/l move   H/J/K/L resize  1-9 jump tab   n/p cycle    N new workspace
  w/W workspace  b sidebar       g navigate     m mouse      ? help   q detach
  G git UI

Mouse is on by default: click a pane, tab, workspace or agent, drag a divider.
Shift+drag for your terminal's own text selection.

Config lives in ~/.rook/config.json and ~/.rook/hotkeys.json; every binding
above is remappable, prefix included. The agent a fan-out launches is
config.json's "agent": {"command": "claude", "args": []}.

Session defaults to $ROOK_SESSION, else "default". Panes get ROOK_SESSION,
ROOK_PANE, ROOK_TAB, ROOK_WORKSPACE and ROOK_ENV=1 in their environment, so an
agent inside a pane can drive rookery with no configuration at all.
`)
}
