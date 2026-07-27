package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/ndjson"
	"github.com/jirkab/rookery/internal/session"
)

// RunWatch implements `rook watch` — stream agent state changes as they
// happen, one JSON object per line.
//
// The point is to remove polling from anything built on top: an outer agent, a
// CI job or a shell loop can block on real events instead of asking `pane ls`
// every second and hoping it catches the moment.
func RunWatch(args []string) error {
	fs := newPaneFlags("watch")
	statuses := fs.set.String("status", "", "only these agent statuses, comma separated: idle,working,blocked,done")
	panes := fs.set.String("pane", "", "only these panes, comma separated")
	kinds := fs.set.String("kind", "", "only these event kinds: agent_status,pane_new,pane_closed,pane_exit")
	plain := fs.set.Bool("plain", false, "human-readable lines instead of JSON")
	if err := fs.parse(args); err != nil {
		return err
	}

	// A watch holds its connection open for as long as you leave it running,
	// so it cannot go through apiCall's request/response path.
	conn, err := net.DialTimeout("unix", session.APISocketPath(fs.session), 2*time.Second)
	if err != nil {
		return fmt.Errorf("no daemon running for session %q: %w", fs.session, err)
	}
	defer conn.Close()

	req := apiproto.Request{
		ID:     "watch",
		Method: "watch",
		Params: mustJSON(apiproto.WatchParams{
			Panes:    splitList(*panes),
			Statuses: splitList(*statuses),
			Kinds:    splitList(*kinds),
		}),
	}
	if err := ndjson.NewWriter(conn).WriteJSON(req); err != nil {
		return err
	}

	r := ndjson.NewReader(conn)

	// The daemon acknowledges before any event, so "connected but quiet" is
	// distinguishable from "not connected".
	var ack apiproto.Response
	if err := r.ReadJSON(&ack); err != nil {
		return err
	}
	if ack.Error != nil {
		return respError(ack)
	}

	for {
		var ev apiproto.Event
		if err := r.ReadJSON(&ev); err != nil {
			// EOF is the daemon shutting down, which is a clean end to a watch.
			return nil
		}
		if *plain {
			fmt.Println(formatEvent(ev))
			continue
		}
		line, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		fmt.Println(string(line))
	}
}

func formatEvent(ev apiproto.Event) string {
	at := ev.At
	if len(at) >= 19 {
		at = at[11:19] // just the clock; the date is today by definition
	}
	who := ev.Label
	if ev.Agent != "" {
		who = ev.Agent + " " + ev.Label
	}
	switch ev.Kind {
	case apiproto.EventStatus:
		return fmt.Sprintf("%s  %-12s %-9s %s → %s", at, ev.PaneID, who, ev.Previous, ev.Status)
	case apiproto.EventPaneExit:
		code := ""
		if ev.ExitCode != nil {
			code = fmt.Sprintf(" (code %d)", *ev.ExitCode)
		}
		return fmt.Sprintf("%s  %-12s %-9s exited%s", at, ev.PaneID, who, code)
	default:
		return fmt.Sprintf("%s  %-12s %-9s %s", at, ev.PaneID, who, ev.Kind)
	}
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func watchUsage() {
	fmt.Fprint(os.Stderr, `rook watch — stream agent state changes, one JSON object per line

Usage:
  rook watch [--status done,blocked] [--pane w1:p2] [--kind agent_status] [--plain]

Events: agent_status (with previous → status), pane_new, pane_closed, pane_exit.
Each line is flat JSON, so every field is one jq hop away:

  rook watch --status blocked | while read -r ev; do
    say "$(echo "$ev" | jq -r .label) needs you"
  done

  rook watch --status done --kind agent_status --plain

The stream replaces polling: it exists so an outer agent or a CI job can block
on the moment an agent finishes rather than asking every second and hoping.
Events are dropped rather than queued without bound if a consumer falls behind,
because a slow pipeline must never be able to stall the multiplexer.
`)
}
