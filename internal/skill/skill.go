// Package skill embeds the agent instruction file and installs it where a
// coding agent will read it.
//
// Same idea as Herdr's SKILL.md: an agent dropped into a pane has the CLI on
// its PATH and the environment telling it which pane it is, and no reason to
// think either matters. The skill is what turns "there is a rook binary here"
// into "I can hand this task to a sibling and wait for it".
package skill

import (
	"embed"
	"os"
	"path/filepath"
)

//go:embed SKILL.md
var files embed.FS

// Markdown returns the skill file's contents.
func Markdown() string {
	data, err := files.ReadFile("SKILL.md")
	if err != nil {
		return ""
	}
	return string(data)
}

// PathIn returns where the skill file belongs inside an agent config
// directory. Skills sit beside settings, so an agent with several config
// directories needs the same targeting the integration installer has.
func PathIn(configDir string) string {
	return filepath.Join(configDir, "skills", "rookery", "SKILL.md")
}

// Install writes the skill file, overwriting any previous copy — it is
// generated, so there is nothing of the user's in it to preserve.
func Install(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(Markdown()), 0o644)
}
