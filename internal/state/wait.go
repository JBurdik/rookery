package state

import (
	"slices"
	"time"

	"github.com/jirkab/rookery/internal/apiproto"
)

// defaultWaitTimeout caps a wait that didn't ask for one. Long enough for a
// real agent turn, short enough that a forgotten wait doesn't hang a script
// forever.
const defaultWaitTimeout = 5 * time.Minute

// waiter is a parked `wait.pane` call: the reply channel of an API request
// that will be answered later, when the pane it is watching reaches one of
// the wanted states (or the deadline passes).
//
// This is why handleAPI can return "deferred": the daemon is a single
// goroutine, so a wait that actually blocked inside the loop would freeze
// every pane in the session. Parking the channel keeps the loop running and
// lets the API connection's own goroutine do the blocking.
type waiter struct {
	id       string
	paneID   string
	states   []string // agent statuses to wait for; empty means "wait for exit"
	reply    chan apiproto.Response
	started  time.Time
	deadline time.Time
}

func (l *Loop) waitPane(id string, p apiproto.WaitPaneParams, reply chan apiproto.Response) (apiproto.Response, bool) {
	// Resolve through the same path every other command uses, so a short id
	// ("p1") works here too — and store the full one, because the waiter
	// outlives this call and looks the pane up again later.
	pane := l.app.resolvePane(p.PaneID)
	if pane == nil {
		return errResp(id, apiproto.ErrPaneNotFound, "no such pane: "+p.PaneID), false
	}

	w := &waiter{
		id:      id,
		paneID:  pane.ID,
		states:  p.AgentStatus,
		reply:   reply,
		started: time.Now(),
	}
	switch {
	case p.TimeoutMS > 0:
		w.deadline = w.started.Add(time.Duration(p.TimeoutMS) * time.Millisecond)
	case p.TimeoutMS == 0:
		w.deadline = w.started.Add(defaultWaitTimeout)
		// negative: no deadline
	}

	// A pane already in a wanted state matches immediately — same semantics
	// as Herdr, and the only ones that make "wait for idle" usable when the
	// agent finished before you asked.
	if w.matches(pane) {
		return ok(id, w.result(pane, false)), false
	}

	l.app.waiters = append(l.app.waiters, w)
	return apiproto.Response{}, true
}

// matches reports whether the pane is in one of the states this waiter wants.
func (w *waiter) matches(pane *Pane) bool {
	if len(w.states) == 0 {
		return pane.Status == "exited"
	}
	if pane.Status == "exited" {
		// A dead pane will never reach any agent status, so completing the
		// wait beats hanging until the timeout. Callers see status "exited"
		// in the result and can tell the difference.
		return true
	}
	return slices.Contains(w.states, string(pane.agentStatus()))
}

func (w *waiter) result(pane *Pane, timedOut bool) apiproto.WaitPaneResult {
	res := apiproto.WaitPaneResult{
		PaneID:      w.paneID,
		Matched:     !timedOut,
		AgentStatus: string(pane.agentStatus()),
		Status:      pane.Status,
		TimedOut:    timedOut,
		WaitedMS:    int(time.Since(w.started).Milliseconds()),
	}
	if pane.Status == "exited" {
		code := pane.ExitCode
		res.ExitCode = &code
	}
	return res
}

// checkWaiters answers every parked wait whose condition now holds. Called
// after anything that can change a pane's status.
func (l *Loop) checkWaiters() {
	if len(l.app.waiters) == 0 {
		return
	}
	l.app.waiters = slices.DeleteFunc(l.app.waiters, func(w *waiter) bool {
		pane, exists := l.app.panes[w.paneID]
		if !exists {
			// The pane was closed out from under the wait.
			w.reply <- errResp(w.id, apiproto.ErrPaneNotFound, "pane closed while waiting: "+w.paneID)
			return true
		}
		if !w.matches(pane) {
			return false
		}
		w.reply <- ok(w.id, w.result(pane, false))
		return true
	})
}

// expireWaiters answers the waits that ran out of time.
func (l *Loop) expireWaiters() {
	if len(l.app.waiters) == 0 {
		return
	}
	now := time.Now()
	l.app.waiters = slices.DeleteFunc(l.app.waiters, func(w *waiter) bool {
		if w.deadline.IsZero() || now.Before(w.deadline) {
			return false
		}
		pane, exists := l.app.panes[w.paneID]
		if !exists {
			w.reply <- errResp(w.id, apiproto.ErrPaneNotFound, "pane closed while waiting: "+w.paneID)
			return true
		}
		w.reply <- apiproto.Response{
			ID:     w.id,
			Result: w.result(pane, true),
			Error:  &apiproto.ErrorBody{Code: apiproto.ErrTimeout, Message: "timed out waiting for pane " + w.paneID},
		}
		return true
	})
}

// failPendingWaiters unblocks every parked caller on shutdown, so no API
// connection is left hanging on a daemon that is going away.
func (l *Loop) failPendingWaiters() {
	for _, w := range l.app.waiters {
		w.reply <- errResp(w.id, apiproto.ErrInternal, "server shutting down")
	}
	l.app.waiters = nil
}
