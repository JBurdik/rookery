package cli

import (
	"fmt"

	"github.com/jirkab/rookery/internal/apiproto"
	"github.com/jirkab/rookery/internal/session"
)

// RunKill implements `rook kill [session]`.
func RunKill(args []string) error {
	name := sessionArg(args)

	if !session.IsLive(session.APISocketPath(name)) {
		session.Cleanup(name)
		fmt.Printf("session %q was not running; cleaned up leftover files\n", name)
		return nil
	}

	resp, err := apiCall(name, apiproto.Request{ID: "kill", Method: "server.shutdown"})
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}
	fmt.Printf("session %q stopped\n", name)
	return nil
}
