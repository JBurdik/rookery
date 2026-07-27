package state

import (
	"time"

	"github.com/jirkab/rookery/internal/agentstatus"
	"github.com/jirkab/rookery/internal/apiproto"
)

// Watchers turn the daemon's internal transitions into an event stream, so an
// outer agent or a CI job can react to agents finishing instead of polling
// `pane ls` in a loop.
//
// This is the one API call that does not answer and hang up: the connection
// stays open and the daemon keeps writing to it. Like waiters, the reply
// channel is parked — but unlike a waiter it is never removed until the client
// goes away.

// watcher is one attached event stream.
type watcher struct {
	id     uint64
	events chan apiproto.Event
	// filters, empty meaning "everything"
	panes    []string
	statuses []string
	kinds    []string
}

// AddWatcher registers an event stream and returns the channel to read it
// from. Called from the API server's connection goroutine; the channel is
// closed when the watcher is removed, which is what ends the stream.
func (l *Loop) AddWatcher(panes, statuses, kinds []string) chan apiproto.Event {
	reply := make(chan chan apiproto.Event, 1)
	l.watchAdd <- watchAddMsg{panes: panes, statuses: statuses, kinds: kinds, reply: reply}
	return <-reply
}

// RemoveWatcher drops a stream when its client disconnects.
func (l *Loop) RemoveWatcher(ch chan apiproto.Event) {
	l.watchDel <- ch
}

type watchAddMsg struct {
	panes    []string
	statuses []string
	kinds    []string
	reply    chan chan apiproto.Event
}

func (l *Loop) handleWatchAdd(m watchAddMsg) {
	l.app.nextWatcher++
	// Buffered: a slow consumer must not be able to stall the daemon's event
	// loop, and events are cheap to drop compared with freezing every pane.
	ch := make(chan apiproto.Event, 64)
	l.app.watchers = append(l.app.watchers, &watcher{
		id:       l.app.nextWatcher,
		events:   ch,
		panes:    m.panes,
		statuses: m.statuses,
		kinds:    m.kinds,
	})
	m.reply <- ch
}

func (l *Loop) handleWatchDel(ch chan apiproto.Event) {
	for i, w := range l.app.watchers {
		if w.events == ch {
			l.app.watchers = append(l.app.watchers[:i], l.app.watchers[i+1:]...)
			close(w.events)
			return
		}
	}
}

// emit publishes an event to every interested watcher.
//
// Drops rather than blocks when a consumer is behind: `rook watch | slow-thing`
// must never be able to freeze the multiplexer. The dropped count is reported
// on the next event that fits, so a consumer can tell it missed something.
func (l *Loop) emit(ev apiproto.Event) {
	if len(l.app.watchers) == 0 {
		return
	}
	ev.At = time.Now().Format("2006-01-02T15:04:05.000Z07:00")
	ev.Session = l.app.Session

	for _, w := range l.app.watchers {
		if !w.wants(ev) {
			continue
		}
		select {
		case w.events <- ev:
		default:
			l.app.watchDropped++
		}
	}
}

func (w *watcher) wants(ev apiproto.Event) bool {
	return matchesFilter(w.kinds, ev.Kind) &&
		matchesFilter(w.panes, ev.PaneID) &&
		// A status filter only applies to events that carry one, so asking for
		// --status done still lets pane_exit through.
		(ev.Status == "" || matchesFilter(w.statuses, ev.Status))
}

func matchesFilter(filter []string, value string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, f := range filter {
		if f == value {
			return true
		}
	}
	return false
}

// closeWatchers ends every stream on shutdown, so `rook watch` exits instead
// of hanging on a socket nobody is going to write to again.
func (l *Loop) closeWatchers() {
	for _, w := range l.app.watchers {
		close(w.events)
	}
	l.app.watchers = nil
}

// --- the events themselves ---

// setAgentState is the only way a pane's agent state changes.
//
// Funnelling every write through here is what keeps the event stream honest:
// a state set directly — `pane send` marking a pane busy, for instance —
// produced a stream with two "working → idle" transitions and no "idle →
// working" between them, because one of the two changes was invisible.
func (l *Loop) setAgentState(pane *Pane, next agentstatus.State) bool {
	if pane.AgentState == next {
		return false
	}
	from := string(pane.AgentState)
	pane.AgentState = next
	l.emit(apiproto.Event{
		Kind:     apiproto.EventStatus,
		PaneID:   pane.ID,
		Label:    pane.displayName(),
		Agent:    pane.Agent,
		Status:   string(next),
		Previous: from,
		Fan:      pane.Fan,
	})
	return true
}

func (l *Loop) emitPaneEvent(kind string, pane *Pane) {
	ev := apiproto.Event{
		Kind:   kind,
		PaneID: pane.ID,
		Label:  pane.displayName(),
		Agent:  pane.Agent,
		Status: string(pane.agentStatus()),
		Fan:    pane.Fan,
	}
	if pane.Status == "exited" {
		code := pane.ExitCode
		ev.ExitCode = &code
	}
	l.emit(ev)
}
