package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jirkab/rookery/internal/update"
)

// RunUpdate checks GitHub Releases and, unless --check is supplied, installs
// the latest verified binary. It deliberately leaves running daemons alone:
// stopping an agent's PTY is a user decision.
func RunUpdate(args []string) error {
	checkOnly := len(args) == 1 && args[0] == "--check"
	if len(args) != 0 && !checkOnly {
		return fmt.Errorf("usage: rook update [--check]")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	tag, available, err := update.Check(ctx, nil, Version)
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}
	if !available {
		fmt.Printf("rook v%s is up to date\n", Version)
		return nil
	}
	if checkOnly {
		fmt.Printf("rook v%s is available (current: v%s)\n", tag, Version)
		return nil
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find current executable: %w", err)
	}
	fmt.Printf("updating rook v%s → %s...\n", Version, tag)
	if err := update.Install(ctx, nil, tag, executable); err != nil {
		return err
	}
	fmt.Printf("updated to %s\n", tag)
	fmt.Println("Running daemons keep their current build; run `rook kill` and reattach when you are ready.")
	return nil
}
