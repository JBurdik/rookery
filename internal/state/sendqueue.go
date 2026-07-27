package state

import (
	"time"

	"github.com/jirkab/rookery/internal/agentstatus"
	"github.com/jirkab/rookery/internal/apiproto"
)

// agentWarmup is how long after spawning an agent we refuse to type at it,
// however idle it looks.
//
// An agent's screen carries no "working" marker while its UI is still coming
// up, so the status detector reasonably calls it idle — and text typed into a
// composer that does not exist yet is simply lost. This is the difference
// between a fan-out where every agent got its prompt and one where three of
// five silently did nothing.
const agentWarmup = 2500 * time.Millisecond

// queuedSend is one thing waiting to be typed at a pane.
type queuedSend struct {
	text string
	// reply asks for the pane's answer to be reported when its turn ends.
	// Used by the manager bar; a fan-out prompt does not want it.
	reply bool
}

// queueSend parks text for a pane instead of writing it now.
//
// Writing immediately is wrong for an agent in two ways that only show up
// under load: a message sent before the UI exists is lost, and two messages
// sent back to back arrive concatenated because the first one's Enter has not
// landed yet. Draining one message per idle turn fixes both, and is why
// fanning a prompt out to five agents reliably reaches all five.
func (l *Loop) queueSend(paneID, text string, wantReply bool) {
	if l.app.sendQueue == nil {
		l.app.sendQueue = map[string][]queuedSend{}
	}
	l.app.sendQueue[paneID] = append(l.app.sendQueue[paneID], queuedSend{text: text, reply: wantReply})
}

// pumpSends drains one queued message per ready pane. Called from the status
// tick, so readiness is re-checked four times a second.
func (l *Loop) pumpSends() {
	for paneID, queue := range l.app.sendQueue {
		if len(queue) == 0 {
			delete(l.app.sendQueue, paneID)
			continue
		}
		pane := l.app.panes[paneID]
		if pane == nil || pane.Status == "exited" {
			// Nobody left to talk to.
			delete(l.app.sendQueue, paneID)
			continue
		}
		if !l.paneReadyForInput(pane) {
			continue
		}

		msg := queue[0]
		if len(queue) == 1 {
			delete(l.app.sendQueue, paneID)
		} else {
			l.app.sendQueue[paneID] = queue[1:]
		}
		if msg.reply {
			l.app.managerAwaiting = true
		}
		l.paneSendKeys("queued", apiproto.PaneSendKeysParams{
			PaneID: pane.ID, Text: msg.text, PressEnter: true,
		})
	}
}

// paneReadyForInput reports whether a pane will actually receive what we type.
func (l *Loop) paneReadyForInput(pane *Pane) bool {
	if time.Since(pane.CreatedAt) < agentWarmup {
		return false
	}
	if time.Now().Before(pane.BusyUntil) {
		return false
	}
	// A shell is ready whenever it is not running a job; an agent has to be at
	// its prompt.
	if pane.Agent == "" {
		return !pane.Actor.Busy()
	}
	return pane.AgentState == agentstatus.Idle
}
