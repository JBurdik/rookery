package cli

import (
	_ "embed"
	"fmt"
	"os"
)

// The completion scripts intentionally contain only the stable CLI grammar.
// They do not contact a daemon, so sourcing them remains fast and works when
// rookery is not running.

//go:embed completions/rook.bash
var bashCompletion string

//go:embed completions/_rook
var zshCompletion string

// RunCompletion writes a shell completion script suitable for eval or for
// installing in the shell's completion directory.
func RunCompletion(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: rook completion <bash|zsh>")
	}

	switch args[0] {
	case "bash":
		_, err := fmt.Fprint(os.Stdout, bashCompletion)
		return err
	case "zsh":
		_, err := fmt.Fprint(os.Stdout, zshCompletion)
		return err
	default:
		return fmt.Errorf("unknown shell %q (want bash or zsh)", args[0])
	}
}
