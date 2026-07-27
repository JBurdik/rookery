package cli

import (
	"fmt"

	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/session"
)

// RunLs implements `rook ls`.
func RunLs(args []string) error {
	names, err := session.List()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Println("no sessions")
		return nil
	}

	for _, name := range names {
		if !session.IsLive(session.APISocketPath(name)) {
			fmt.Printf("%-20s STALE\n", name)
			continue
		}

		resp, err := apiCall(name, apiproto.Request{ID: "ls", Method: "pane.list"})
		if err != nil || resp.Error != nil {
			fmt.Printf("%-20s RUNNING (pane.list failed)\n", name)
			continue
		}
		var result apiproto.PaneListResult
		if err := decodeResult(resp.Result, &result); err != nil {
			fmt.Printf("%-20s RUNNING (unreadable pane list)\n", name)
			continue
		}
		fmt.Printf("%-20s RUNNING  panes=%d\n", name, len(result.Panes))
	}
	return nil
}
