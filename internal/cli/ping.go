package cli

import (
	"fmt"

	"github.com/jirkab/rookery/internal/apiproto"
)

// RunPing implements `rook ping [session]` — a quick daemon liveness check
// that doubles as a way to confirm the API socket is genuinely reachable.
func RunPing(args []string) error {
	name := sessionArg(args)

	resp, err := apiCall(name, apiproto.Request{ID: "ping", Method: "ping"})
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result apiproto.PingResult
	if err := decodeResult(resp.Result, &result); err != nil {
		return err
	}
	fmt.Printf("pong from session %q (protocol %d, version %s)\n", name, result.Protocol, result.Version)
	return nil
}
