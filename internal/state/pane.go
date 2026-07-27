package state

import (
	"time"

	"github.com/jirkab/rookery/internal/agentstatus"
	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/attachproto"
	"github.com/jirkab/rookery/internal/pty"
	"github.com/jirkab/rookery/internal/termgrid"
)

// Pane is one agent/command running under a PTY.
type Pane struct {
	ID        string
	Label     string
	Cmd       string
	Args      []string
	Cwd       string
	Grid      *termgrid.Grid
	Actor     *pty.Actor
	Status    string // "running" | "exited"
	ExitCode  int
	CreatedAt time.Time
	Revision  uint64

	// Agent is the detected agent name ("claude", "codex", …), or "" for a
	// plain command.
	Agent string
	// AgentState is the detector's latest verdict, before the seen/unseen
	// distinction is applied — see agentStatus.
	AgentState agentstatus.State
	// Seen records whether a human has looked at this pane since it last
	// finished working. An unseen completion reports as "done", a seen one as
	// "idle"; that difference is the whole point of the status column, since
	// it answers "which agent is waiting for me?" at a glance.
	Seen bool
	// LastOutput is when the PTY last produced bytes — the fallback
	// working/idle signal for panes whose agent isn't recognised.
	LastOutput time.Time
	// Running is the executable currently owning the PTY, which is not the
	// same thing as Cmd: a pane spawned as a shell shows "claude" here once
	// you type `claude` into it. Both the display name and agent detection
	// follow this, so an agent started by hand is treated exactly like one
	// spawned by `rook pane new -- claude`.
	Running string
	// BusyUntil suppresses an "idle" verdict for a moment after input is
	// sent. An agent takes a beat to repaint, so the tick right after a
	// prompt still shows the old idle screen — without this grace period
	// that reads as "finished already", which pings you for nothing and
	// lets `wait --status done` return before the work even starts.
	BusyUntil time.Time
	// Reported is a status an agent integration told us directly, rather than
	// one inferred from its screen. When fresh it wins: a hook firing on
	// "permission dialog opened" is ground truth, where screen-scraping is a
	// heuristic that can only ever approximate it.
	Reported     agentstatus.State
	ReportedAt   time.Time
	AgentSession string
	// Fan, Branch and Worktree tie a pane to a fan-out run: which group it
	// belongs to, and the checkout its agent is working in.
	Fan      string
	Branch   string
	Worktree string
	// Manager marks the pane holding the manager agent — the one the command
	// bar talks to. Only rookery's memory of which pane it is makes it
	// special; it is an ordinary pane otherwise.
	Manager bool
	// DoneAt is when a turn last ended. The pane's border blinks for a few
	// seconds afterwards: a badge you have to be looking at is no use if the
	// thing that changed is on the screen you are already staring at.
	DoneAt time.Time
	// View is the pane's scroll/copy viewport. Inactive means "draw the live
	// screen", which is every pane almost all of the time.
	View scrollView
	// Title is the agent's own terminal title, spinner stripped. Agents put
	// the current task there ("Count from 1 to 30"), which is far more use in
	// a sidebar than the executable's name repeated once per pane.
	Title string
}

// agentStatus is the pane's externally visible status, folding the
// seen/unseen distinction into the detector's state.
//
// "Done" is an attention state — finished, and nobody has looked — so it is
// only ever reported for a pane that actually holds an agent. A shell or a
// test runner just goes quiet; calling that "done, come look" would make the
// sidebar cry wolf on every prompt.
func (p *Pane) agentStatus() agentstatus.State {
	switch {
	case p.Status == "exited":
		return agentstatus.Done
	case p.Agent != "" && p.AgentState == agentstatus.Idle && !p.Seen:
		return agentstatus.Done
	default:
		return p.AgentState
	}
}

func (p *Pane) toInfo() apiproto.PaneInfo {
	cols, rows := p.Grid.Size()
	info := apiproto.PaneInfo{
		PaneID:      p.ID,
		Label:       p.Label,
		Cmd:         p.Cmd,
		Args:        p.Args,
		Cwd:         p.Cwd,
		Status:      p.Status,
		Agent:       p.Agent,
		AgentStatus: string(p.agentStatus()),
		Cols:        cols,
		Rows:        rows,
		PID:         p.Actor.PID(),
		CreatedAt:   p.CreatedAt.Format(time.RFC3339),
		Revision:    p.Revision,
	}
	if p.Status == "exited" {
		code := p.ExitCode
		info.ExitCode = &code
	}
	return info
}

func (p *Pane) toSummary() attachproto.PaneSummary {
	return attachproto.PaneSummary{
		PaneID:      p.ID,
		Label:       p.Label,
		Cmd:         p.Cmd,
		Title:       p.displayName(),
		Status:      p.Status,
		Agent:       p.Agent,
		AgentStatus: string(p.agentStatus()),
	}
}

// displayName is what the pane is called in the UI, most specific first: a
// label set by hand, then what the agent says it is doing, then the program
// running in it, then whatever it was spawned as.
//
// The agent's own title beats its executable name because it carries the
// task — "Count from 1 to 30" tells you which of four Claude panes you are
// looking at; four rows reading "claude" do not. Only agent panes get this:
// shells set their title to a truncated path, which is noisier than "zsh".
func (p *Pane) displayName() string {
	switch {
	case p.Label != "":
		return p.Label
	case p.Agent != "" && p.Title != "":
		return p.Title
	case p.Running != "":
		return p.Running
	default:
		return baseName(p.Cmd)
	}
}

func baseName(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return s[i+1:]
		}
	}
	return s
}
