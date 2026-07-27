package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jirkab/rookery/internal/integration"
	"github.com/jirkab/rookery/internal/skill"
)

// RunSkill implements `rook skill` — print or install the agent instruction
// file that teaches an agent to drive rookery from inside a pane.
func RunSkill(args []string) error {
	fs := newPaneFlags("skill")
	install := fs.set.Bool("install", false, "write it into the agent's skill directory")
	var target targetFlags
	target.register(fs.set)
	if err := fs.parse(args); err != nil {
		return err
	}

	if !*install {
		fmt.Print(skill.Markdown())
		return nil
	}

	// Skills live beside settings, so the same targeting applies: with several
	// config directories, installing into the wrong one is silent.
	spec := integration.Specs["claude"]
	paths, err := target.resolve(spec)
	if err != nil {
		return err
	}
	for _, settings := range paths {
		path := skill.PathIn(filepath.Dir(settings))
		if err := skill.Install(path); err != nil {
			return err
		}
		fmt.Printf("installed the rookery skill: %s\n", path)
	}
	if !target.all && spec.ConfigEnv != "" && os.Getenv(spec.ConfigEnv) != "" {
		fmt.Printf("  (%s is set, so that is the config used; --all covers the others)\n", spec.ConfigEnv)
	}
	fmt.Println("\nagents started from now on will know how to drive rookery.")
	return nil
}
