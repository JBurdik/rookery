package state

import (
	"strings"

	"github.com/jirkab/rookery/internal/apiproto"
)

// managerTabName is the tab the manager agent lives in. Its own tab rather
// than a split, so asking it something never rearranges the panes you were
// looking at.
const managerTabName = "manager"

// managerBriefing is sent as the manager's first message. It exists because an
// agent dropped into a pane has no idea it is inside a multiplexer it can
// drive — the CLI is on its PATH and the environment says which pane it is,
// but nothing points that out.
const managerBriefing = `You are the manager agent inside rookery, a terminal multiplexer for coding agents. ` +
	`You control it with the "rook" CLI, which is on your PATH. Useful commands: ` +
	`"rook pane ls" (panes with agent status), ` +
	`"rook pane new --label NAME -- CMD" (spawn a pane, e.g. -- claude), ` +
	`"rook pane send PANE text..." (type into a pane and press enter), ` +
	`"rook pane read PANE --raw" (read a pane's screen), ` +
	`"rook wait agent-status PANE --status done --timeout 300000" (block until an agent finishes), ` +
	`"rook workspace ls", "rook tab new NAME", "rook pane kill PANE". ` +
	`Every command prints JSON. Your own ids are in $ROOK_PANE and $ROOK_WORKSPACE. ` +
	`When I ask for something, use those commands to do it, then tell me briefly what you did. Reply "ready" now.`

// managerSend hands a line of text to the manager agent, starting it first if
// it isn't running.
//
// The manager is an ordinary pane: it has the CLI, the env vars and the same
// permissions as any other agent, which is exactly why this needs no new
// machinery. The only special thing about it is that rookery remembers which
// pane it is.
func (l *Loop) managerSend(text, managerCmd string) apiproto.Response {
	pane := l.managerPane()
	if pane == nil {
		resp := l.startManager(managerCmd)
		if resp.Error != nil {
			return resp
		}
		pane = l.managerPane()
		if pane == nil {
			return errResp("manager", apiproto.ErrInternal, "manager pane vanished after starting")
		}
		// The briefing goes first, so the agent knows what it is for before
		// it sees the actual request.
		l.paneSendKeys("manager", apiproto.PaneSendKeysParams{
			PaneID: pane.ID, Text: managerBriefing, PressEnter: true,
		})
	}
	if strings.TrimSpace(text) == "" {
		return ok("manager", apiproto.PaneSendKeysResult{OK: true})
	}
	return l.paneSendKeys("manager", apiproto.PaneSendKeysParams{
		PaneID: pane.ID, Text: text, PressEnter: true,
	})
}

// managerPane finds the running manager, if there is one.
func (l *Loop) managerPane() *Pane {
	for _, id := range l.app.allPanes() {
		pane := l.app.panes[id]
		if pane != nil && pane.Manager && pane.Status != "exited" {
			return pane
		}
	}
	return nil
}

// startManager opens the manager in its own tab, without stealing focus:
// asking it to do something should not move you away from your own work.
func (l *Loop) startManager(managerCmd string) apiproto.Response {
	if managerCmd == "" {
		managerCmd = "claude"
	}
	w := l.app.activeWorkspace()
	if w == nil {
		return errResp("manager", apiproto.ErrNotFound, "no workspace to start the manager in")
	}
	previousTab := w.activeTab

	tab := w.addTab(managerTabName)
	resp := l.paneCreate("manager", apiproto.PaneCreateParams{
		Cmd:     managerCmd,
		Cwd:     w.Cwd,
		Label:   managerTabName,
		NoFocus: true,
	})
	if resp.Error != nil {
		w.removeTab(tab.ID)
		w.activeTab = previousTab
		return resp
	}
	if info, isInfo := resp.Result.(apiproto.PaneInfo); isInfo {
		if pane := l.app.panes[info.PaneID]; pane != nil {
			pane.Manager = true
		}
	}
	// Stay where the user was.
	w.activeTab = previousTab
	l.app.dirty = true
	l.broadcastState()
	return resp
}
