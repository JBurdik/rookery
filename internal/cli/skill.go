package cli

import (
	"fmt"
	"os"
	"sort"

	"github.com/jirkab/rookery/internal/skill"
)

// RunSkill implements `rook skill` — print or install the agent instruction
// file that teaches an agent to drive rookery from inside a pane.
func RunSkill(args []string) error {
	fs := newPaneFlags("skill")
	install := fs.set.Bool("install", false, "write it into the agent skill directories")
	if err := fs.parse(args); err != nil {
		return err
	}

	if !*install {
		fmt.Print(skill.Markdown())
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	targets := skill.Targets(home)
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		path := targets[name]
		if err := skill.Install(path); err != nil {
			return err
		}
		fmt.Printf("installed the rookery skill for %s: %s\n", name, path)
	}
	fmt.Println("\nagents started from now on will know how to drive rookery.")
	return nil
}
