package state

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/session"
	"github.com/jirkab/rookery/internal/worktree"
)

// A fan-out runs one prompt past several agents at once, each in its own git
// worktree, and then lets you compare the answers.
//
// This is the workflow the whole daemon exists for. Everything it needs was
// already here — panes, tabs, the ready-queue, per-pane cwd, agent status — so
// a fan is mostly bookkeeping: which panes belong to which run, and which
// branch each one is working on.

// fanStart creates the worktrees, the panes, and queues the prompt to each.
func (l *Loop) fanStart(id string, p apiproto.FanStartParams) apiproto.Response {
	if strings.TrimSpace(p.Prompt) == "" {
		return errResp(id, apiproto.ErrInvalidParams, "prompt is required")
	}
	count := p.Agents
	if count <= 0 {
		count = 3
	}
	if count > 12 {
		// Not a hard limit of the design, but twelve agents on one task is
		// almost certainly a typo, and each one is a real process.
		return errResp(id, apiproto.ErrInvalidParams, "at most 12 agents per fan")
	}
	cmd := p.Cmd
	if cmd == "" {
		cmd = "claude"
	}

	w := l.app.activeWorkspace()
	if w == nil {
		return errResp(id, apiproto.ErrNotFound, "no workspace to fan out from")
	}
	name := p.Name
	if name == "" {
		l.app.nextFan++
		name = "fan" + strconv.Itoa(l.app.nextFan)
	}
	if l.fanPanes(name) != nil {
		return errResp(id, apiproto.ErrInvalidParams, "a fan named "+name+" is already running")
	}

	trees := make([]worktree.Tree, count)
	if p.Worktree {
		mgr := worktree.New(session.WorktreeDir())
		for i := range count {
			tree, err := mgr.Create(w.Cwd, fmt.Sprintf("%s-%d", name, i+1), p.Base)
			if err != nil {
				// Undo the ones already made: a half-created fan is worse
				// than none, because you would have to clean it up by hand.
				for j := range i {
					_ = mgr.Remove(w.Cwd, fmt.Sprintf("%s-%d", name, j+1), true)
				}
				return errResp(id, apiproto.ErrInternal, err.Error())
			}
			trees[i] = tree
		}
	}

	previousTab := w.activeTab
	result := apiproto.FanStartResult{Fan: name, Prompt: p.Prompt}

	for i := range count {
		label := fmt.Sprintf("%s-%d", name, i+1)
		cwd := w.Cwd
		if p.Worktree {
			cwd = trees[i].Path
		}

		// One tab per agent: a fan of five in one tab would be five slivers,
		// and you want to look at them one at a time anyway.
		w.addTab(label)
		resp := l.paneCreate("fan", apiproto.PaneCreateParams{
			Cmd:     cmd,
			Args:    p.Args,
			Cwd:     cwd,
			Label:   label,
			NoFocus: true,
		})
		if resp.Error != nil {
			return resp
		}
		info, isInfo := resp.Result.(apiproto.PaneInfo)
		if !isInfo {
			continue
		}
		pane := l.app.panes[info.PaneID]
		if pane == nil {
			continue
		}
		pane.Fan = name
		pane.Branch = trees[i].Branch
		pane.Worktree = trees[i].Path

		// Queued, not written: an agent that has not finished starting loses
		// whatever you type at it.
		l.queueSend(pane.ID, p.Prompt, false)

		result.Panes = append(result.Panes, apiproto.FanPane{
			PaneID:   pane.ID,
			Label:    label,
			Branch:   pane.Branch,
			Worktree: pane.Worktree,
		})
	}

	// Leave the user where they were; a fan-out is something you check on, not
	// something that hijacks your screen.
	w.activeTab = previousTab
	l.app.dirty = true
	l.broadcastState()
	return ok(id, result)
}

// fanList reports every pane in a fan, with what its agent is doing and what
// it has actually changed on disk.
func (l *Loop) fanList(id string, p apiproto.FanListParams) apiproto.Response {
	out := apiproto.FanListResult{}
	seen := map[string]bool{}

	for _, paneID := range l.app.allPanes() {
		pane := l.app.panes[paneID]
		if pane == nil || pane.Fan == "" {
			continue
		}
		if p.Fan != "" && pane.Fan != p.Fan {
			continue
		}
		if !seen[pane.Fan] {
			seen[pane.Fan] = true
			out.Fans = append(out.Fans, pane.Fan)
		}
		entry := apiproto.FanPane{
			PaneID:      pane.ID,
			Fan:         pane.Fan,
			Label:       pane.displayName(),
			Branch:      pane.Branch,
			Worktree:    pane.Worktree,
			Status:      pane.Status,
			AgentStatus: string(pane.agentStatus()),
		}
		if pane.Worktree != "" {
			entry.Diffstat = worktree.Diffstat(pane.Worktree)
		}
		out.Panes = append(out.Panes, entry)
	}
	return ok(id, out)
}

// fanClean closes a fan's panes, its tabs, and the worktrees it created.
func (l *Loop) fanClean(id string, p apiproto.FanCleanParams) apiproto.Response {
	if p.Fan == "" {
		return errResp(id, apiproto.ErrInvalidParams, "fan name is required")
	}
	panes := l.fanPanes(p.Fan)
	if panes == nil {
		return errResp(id, apiproto.ErrNotFound, "no fan named "+p.Fan)
	}

	mgr := worktree.New(session.WorktreeDir())
	result := apiproto.FanCleanResult{Fan: p.Fan}

	// Check every worktree before touching anything. Closing the panes first
	// and then refusing to remove the worktrees left orphaned checkouts with
	// no way back to them — a cleanup either happens or it does not.
	if !p.Force {
		var blocked []string
		for _, pane := range panes {
			if pane.Worktree == "" {
				continue
			}
			if trees, err := mgr.List(pane.Worktree); err == nil {
				for _, t := range trees {
					if t.Name == pane.Label && t.Dirty {
						blocked = append(blocked, t.Name+" has uncommitted changes")
					}
				}
			}
		}
		if len(blocked) > 0 {
			result.Problems = append(blocked,
				"nothing was removed; commit the work you want or pass --force")
			return ok(id, result)
		}
	}

	var problems []string
	for _, pane := range panes {
		repo := ""
		if w, _ := l.app.tabOf(pane.ID); w != nil {
			repo = w.Cwd
		}
		name := pane.Label
		l.paneClose("fan-clean", apiproto.PaneCloseParams{PaneID: pane.ID})
		result.Closed++

		if pane.Worktree == "" || repo == "" {
			continue
		}
		if err := mgr.Remove(repo, name, p.Force); err != nil {
			// Uncommitted work is the usual reason, and losing an agent's
			// output to a stray cleanup would be unforgivable. Say so and
			// leave it alone.
			problems = append(problems, err.Error())
			continue
		}
		result.Removed++
	}
	result.Problems = problems
	l.app.dirty = true
	l.broadcastState()
	return ok(id, result)
}

// fanPanes returns a fan's panes, or nil if there is no such fan.
func (l *Loop) fanPanes(name string) []*Pane {
	var out []*Pane
	for _, id := range l.app.allPanes() {
		if pane := l.app.panes[id]; pane != nil && pane.Fan == name {
			out = append(out, pane)
		}
	}
	return out
}
