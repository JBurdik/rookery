package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jirkab/rookery/internal/integration"
)

// targetFlags select which of an agent's configurations to act on.
//
// Agents routinely have several live at once — one config directory for work,
// another for personal, plus whatever is in the repo — and writing hooks into
// the wrong one is the worst possible outcome: it reports success and changes
// nothing, because that config is never loaded.
type targetFlags struct {
	configDir string
	settings  string
	project   bool
	local     bool
	all       bool
}

func (t *targetFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&t.configDir, "config-dir", "", "the agent config directory to use (e.g. ~/claude-work)")
	fs.StringVar(&t.settings, "settings", "", "the exact settings file to edit")
	fs.BoolVar(&t.project, "project", false, "use ./.claude/settings.json in the current directory")
	fs.BoolVar(&t.local, "local", false, "use ./.claude/settings.local.json (gitignored, highest precedence)")
	fs.BoolVar(&t.all, "all", false, "act on every config directory found")
}

// resolve returns the settings files to act on, most-specific flag winning.
func (t *targetFlags) resolve(spec integration.Spec) ([]string, error) {
	if t.settings != "" {
		return []string{expandHome(t.settings)}, nil
	}

	cwd, _ := os.Getwd()
	switch {
	case t.local:
		return []string{filepath.Join(cwd, spec.ConfigDirName, "settings.local.json")}, nil
	case t.project:
		return []string{filepath.Join(cwd, spec.ConfigDirName, spec.SettingsFile)}, nil
	case t.configDir != "":
		return []string{spec.SettingsIn(expandHome(t.configDir))}, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dirs := spec.ConfigDirs(home, "")
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no %s configuration directory found", spec.Name)
	}
	if t.all {
		out := make([]string, 0, len(dirs))
		for _, dir := range dirs {
			out = append(out, spec.SettingsIn(dir))
		}
		return out, nil
	}
	// The first is the one the agent itself would load: a relocated config
	// directory if the environment names one, otherwise the home directory.
	return []string{spec.SettingsIn(dirs[0])}, nil
}

// candidates lists every config directory worth reporting on, so `status` can
// show which of several configurations has the integration.
func candidates(spec integration.Spec) []string {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	dirs := spec.ConfigDirs(home, cwd)

	// Sibling directories of a relocated config are a common way to keep
	// several profiles ("claude-work", "claude-personal"), and a user who has
	// one almost certainly wants to see the others listed.
	if home != "" {
		if siblings, err := filepath.Glob(filepath.Join(home, "*claude*")); err == nil {
			for _, dir := range siblings {
				if info, err := os.Stat(dir); err != nil || !info.IsDir() {
					continue
				}
				if _, err := os.Stat(spec.SettingsIn(dir)); err != nil {
					continue // not a config directory, just a similarly named folder
				}
				dirs = appendUnique(dirs, dir)
			}
		}
	}
	return dirs
}

func appendUnique(list []string, value string) []string {
	for _, v := range list {
		if v == value {
			return list
		}
	}
	return append(list, value)
}

// expandHome turns a leading ~ into the home directory, since these paths are
// typed by hand and a shell is not always the one expanding them.
func expandHome(path string) string {
	if path == "~" || len(path) > 1 && path[:2] == "~/" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}
