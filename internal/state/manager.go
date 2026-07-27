package state

import (
	"regexp"
	"strings"
	"time"

	"github.com/jirkab/rookery/internal/agentstatus"

	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/attachproto"
)

// managerTabName is the tab the manager agent lives in. Its own tab rather
// than a split, so asking it something never rearranges the panes you were
// looking at.
const managerTabName = "manager"

// managerBriefing is sent as the manager's first message. It exists because an
// agent dropped into a pane has no idea it is inside a multiplexer it can
// drive — the CLI is on its PATH and the environment says which pane it is,
// but nothing points that out.
// Kept short on purpose: it is typed into an agent's composer, so a wall of
// text is both slow to send and ugly to look at. `rook --help` covers the rest
// if the agent wants detail.
const managerBriefing = `You are the manager agent inside rookery, a terminal multiplexer. ` +
	`Drive it with the "rook" CLI on your PATH: pane ls / pane new --label X -- CMD / ` +
	`pane send PANE text / pane read PANE --raw / wait agent-status PANE --status done / ` +
	`tab new NAME / workspace ls. All output is JSON; "rook --help" has the rest. ` +
	`You are $ROOK_PANE. Do what I ask with those commands, then answer in one short line.`

// managerWarmup is how long after spawning the manager we refuse to type at
// it, however idle it claims to look.
//
// An agent's screen has no "working" marker while its UI is still coming up,
// so the status detector reasonably calls it idle — and text typed into a
// composer that does not exist yet is simply lost. Two seconds is the
// difference between a prompt that runs and one that vanishes.
const managerWarmup = 2500 * time.Millisecond

// managerMsg is one queued thing to say to the manager.
type managerMsg struct {
	text string
	// user distinguishes a real request from the briefing, so only an answer
	// to something you actually asked comes back to the bar.
	user bool
}

// managerSend queues a request for the manager agent, starting it if needed.
//
// Queued rather than written straight out, for two reasons learned the hard
// way: the agent needs to have finished starting before anything typed at it
// registers, and two messages sent back to back arrived concatenated in its
// composer — the first one's Enter had not landed yet. The queue drains one
// message per idle turn, which fixes both.
//
// The manager is otherwise an ordinary pane: same CLI, same env, same
// permissions as any agent you start yourself. The only special thing is that
// rookery remembers which pane it is.
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
		// The briefing goes first, so the agent knows what it is for before it
		// sees the request.
		l.app.managerQueue = append(l.app.managerQueue, managerMsg{text: managerBriefing})
	}
	if strings.TrimSpace(text) != "" {
		l.app.managerQueue = append(l.app.managerQueue, managerMsg{text: text, user: true})
	}
	l.pumpManager()
	return ok("manager", apiproto.ManagerSendResult{
		PaneID: pane.ID,
		Queued: len(l.app.managerQueue),
	})
}

// pumpManager sends the next queued message once the manager is ready for it:
// started, idle, and not still digesting the last one. Called from the status
// tick, so "ready" is re-checked four times a second.
func (l *Loop) pumpManager() {
	if len(l.app.managerQueue) == 0 {
		return
	}
	pane := l.managerPane()
	if pane == nil {
		l.app.managerQueue = nil
		return
	}
	if time.Since(pane.CreatedAt) < managerWarmup {
		return
	}
	if pane.AgentState != agentstatus.Idle || time.Now().Before(pane.BusyUntil) {
		return
	}

	msg := l.app.managerQueue[0]
	l.app.managerQueue = l.app.managerQueue[1:]
	l.app.managerAwaiting = msg.user
	l.paneSendKeys("manager", apiproto.PaneSendKeysParams{
		PaneID: pane.ID, Text: msg.text, PressEnter: true,
	})
}

// managerReplied is called when the manager's turn ends. It lifts the last
// line of its answer out of the pane and sends it to the clients' command bar.
func (l *Loop) managerReplied(pane *Pane) {
	if !l.app.managerAwaiting {
		return
	}
	l.app.managerAwaiting = false

	// Deep enough to reach past the composer and status bar to the actual
	// answer, which sits above them.
	reply := lastReplyLine(pane.Grid.BottomLines(40))
	if reply == "" {
		reply = "(no reply — open the manager tab)"
	}
	l.broadcastControl(attachproto.ManagerReply{
		Type: attachproto.TypeManagerReply,
		Text: reply,
	})
}

// replyMarkers are the glyphs agent TUIs prefix their own output with. Claude
// Code uses ⏺.
//
// Deliberately not ✻ or ✳: those are Claude's spinner and timing glyphs, and
// including one made "Churned for 21s" outrank the actual answer sitting one
// line above it.
const replyMarkers = "⏺●◼"

// composerMarkers start the input box — the line holding what was *typed*, not
// what was answered. Returning that would echo the question back as the reply.
const composerMarkers = "❯>›»"

// timingFooter matches "Churned for 21s" and its many synonyms, with or
// without a leading glyph.
var timingFooter = regexp.MustCompile(`(?i)^[^\p{L}]*\p{L}+ for \d+m?\s?\d*s$`)

// lastReplyLine picks an agent's answer out of the bottom of its screen.
//
// Preference order matters. A marked line (Claude's "⏺ …") is unambiguous, so
// it wins; the composer echoes the request and must never be mistaken for an
// answer; everything else an agent draws down there is chrome — the box rules,
// the model and context bar, mode hints.
//
// A heuristic on purpose: the alternative is parsing each agent's UI, and the
// full answer is one keypress away in the manager's own tab.
func lastReplyLine(lines []string) string {
	// Find the newest marked line. Everything after it that is neither chrome
	// nor a new marker is its wrapped continuation — an answer long enough to
	// wrap would otherwise be cut mid-sentence.
	last := -1
	for i := len(lines) - 1; i >= 0; i-- {
		line := normaliseLine(lines[i])
		if _, _, ok := splitMarker(line, replyMarkers); ok {
			last = i
			break
		}
	}

	if last >= 0 {
		_, head, _ := splitMarker(normaliseLine(lines[last]), replyMarkers)
		parts := []string{head}
		for _, raw := range lines[last+1:] {
			line := normaliseLine(raw)
			if line == "" || isChrome(line) {
				break
			}
			if _, _, isMarked := splitMarker(line, replyMarkers); isMarked {
				break
			}
			if _, _, isComposer := splitMarker(line, composerMarkers); isComposer {
				break
			}
			parts = append(parts, line)
		}
		if joined := strings.TrimSpace(strings.Join(parts, " ")); joined != "" {
			return collapse(joined)
		}
	}

	// Nothing marked: the newest line that is neither chrome nor the composer.
	for i := len(lines) - 1; i >= 0; i-- {
		line := normaliseLine(lines[i])
		if line == "" || isChrome(line) {
			continue
		}
		if _, _, isComposer := splitMarker(line, composerMarkers); isComposer {
			continue
		}
		return collapse(line)
	}
	return ""
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// normaliseLine trims a screen line into comparable text. The non-breaking
// space matters: Claude's composer uses one after its prompt glyph, so a plain
// TrimSpace leaves the line looking like content.
func normaliseLine(line string) string {
	line = strings.ReplaceAll(line, " ", " ")
	return strings.TrimSpace(line)
}

// splitMarker reports whether a line starts with one of the given glyphs, and
// returns what follows it.
func splitMarker(line, markers string) (matched bool, rest string, ok bool) {
	for _, m := range markers {
		if prefix, found := strings.CutPrefix(line, string(m)); found {
			return true, strings.TrimSpace(prefix), true
		}
	}
	return false, line, false
}

func isChrome(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range []string{
		"esc to interrupt", "ctx:", "? for shortcuts", "auto mode",
		"bypass permissions", "shift+tab", "transcript saving",
		"claude code v", "welcome back",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	// Claude's post-turn timing footer: "✻ Churned for 21s", "Crunched for
	// 3m 4s". It sits *below* the answer, so without this it wins the "newest
	// line" contest every time.
	if timingFooter.MatchString(line) {
		return true
	}
	// A line of box drawing or rules carries no words.
	return strings.Trim(line, "─━╌╍│┃╭╮╰╯├┤┬┴┼▏▕ ") == ""
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
