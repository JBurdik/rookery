package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jirkab/rookery/internal/apiproto"
)

// RunFan implements `rook fan` — one prompt, several agents, each in its own
// git worktree, then compare what they did.
func RunFan(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "ls", "list", "status":
		return fanList(args[1:])
	case "clean":
		return fanClean(args[1:])
	case "-h", "--help", "help":
		fanUsage()
		return nil
	case "":
		fanUsage()
		return errors.New("fan: nothing to do")
	default:
		// Anything else is the prompt: `rook fan "make the tests pass"`.
		return fanStart(args)
	}
}

func fanStart(args []string) error {
	fs := newPaneFlags("fan")
	agents := fs.set.Int("agents", 3, "how many agents to run")
	cmd := fs.set.String("cmd", "", "agent to run (default: claude)")
	name := fs.set.String("name", "", "name for this run (default: fan1, fan2, …)")
	base := fs.set.String("base", "", "commit-ish the branches start from (default: HEAD)")
	noWorktree := fs.set.Bool("no-worktree", false, "share the current directory instead of one checkout each")
	if err := fs.parse(args); err != nil {
		return err
	}

	prompt := strings.Join(fs.args(), " ")
	if prompt == "" {
		return errors.New(`usage: rook fan "what the agents should do" [--agents N]`)
	}

	resp, err := apiCall(fs.session, apiproto.Request{
		ID:     "fan-start",
		Method: "fan.start",
		Params: mustJSON(apiproto.FanStartParams{
			Prompt:   prompt,
			Agents:   *agents,
			Cmd:      *cmd,
			Name:     *name,
			Base:     *base,
			Worktree: !*noWorktree,
		}),
	})
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return respError(resp)
	}

	var result apiproto.FanStartResult
	if err := decodeResult(resp.Result, &result); err != nil {
		return err
	}
	fmt.Printf("fan %s — %d agents on: %s\n", result.Fan, len(result.Panes), result.Prompt)
	for _, p := range result.Panes {
		line := "  " + p.PaneID + "  " + p.Label
		if p.Branch != "" {
			line += "  " + p.Branch
		}
		fmt.Println(line)
	}
	fmt.Printf("\nthe prompt is queued and goes in as each agent finishes starting up.\n")
	fmt.Printf("watch them:  rook watch --status done,blocked\n")
	fmt.Printf("compare:     rook fan ls %s\n", result.Fan)
	fmt.Printf("tidy up:     rook fan clean %s\n", result.Fan)
	return nil
}

func fanList(args []string) error {
	fs := newPaneFlags("fan ls")
	jsonOut := fs.set.Bool("json", false, "print the raw JSON")
	if err := fs.parse(args); err != nil {
		return err
	}
	fan := ""
	if rest := fs.args(); len(rest) > 0 {
		fan = rest[0]
	}

	resp, err := apiCall(fs.session, apiproto.Request{
		ID:     "fan-ls",
		Method: "fan.list",
		Params: mustJSON(apiproto.FanListParams{Fan: fan}),
	})
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return respError(resp)
	}
	if *jsonOut {
		return printJSON(resp.Result)
	}

	var result apiproto.FanListResult
	if err := decodeResult(resp.Result, &result); err != nil {
		return err
	}
	if len(result.Panes) == 0 {
		fmt.Println("no fan-outs running")
		return nil
	}
	for _, p := range result.Panes {
		status := p.AgentStatus
		if p.Status == "exited" {
			status = "exited"
		}
		fmt.Printf("%-10s %-9s %-9s %-22s %s\n", p.Fan, p.PaneID, status, p.Branch, p.Diffstat)
	}
	return nil
}

func fanClean(args []string) error {
	fs := newPaneFlags("fan clean")
	force := fs.set.Bool("force", false, "discard uncommitted work in the worktrees")
	if err := fs.parse(args); err != nil {
		return err
	}
	rest := fs.args()
	if len(rest) == 0 {
		return errors.New("usage: rook fan clean <name> [--force]")
	}
	return callAndPrint(fs.session, "fan.clean", apiproto.FanCleanParams{Fan: rest[0], Force: *force})
}

func fanUsage() {
	fmt.Fprint(os.Stderr, `rook fan — one prompt, several agents, one checkout each

Usage:
  rook fan "<what to do>" [--agents N] [--cmd claude] [--name NAME]
  rook fan ls [name] [--json]      what each agent is doing, and what it changed
  rook fan clean <name> [--force]  close the panes, remove the worktrees

Flags for a run:
  --agents N       how many agents (default 3)
  --cmd CMD        which agent to run (default claude)
  --base REF       commit-ish the branches start from (default HEAD)
  --no-worktree    share the current directory instead of one checkout each

Each agent gets its own tab, its own git worktree under
~/.local/state/rookery/worktrees, and its own branch "rook/<name>-<n>", so
three agents on the same task cannot fight over the index and comparing their
answers is a diff rather than three transcripts.

The prompt is queued rather than typed immediately: an agent that has not
finished starting loses whatever you send it, which is how a fan-out of five
quietly becomes a fan-out of two.

  rook fan "make the flaky auth test pass" --agents 3
  rook watch --status done,blocked        # tell me when they land
  rook fan ls                             # who did what
  git -C . merge rook/fan1-2              # keep the one you liked
  rook fan clean fan1 --force             # bin the rest
`)
}
