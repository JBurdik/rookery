package state

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Workspace is the top-level container: one per repo, task or investigation.
// It owns tabs, tabs own a layout of panes. Same shape as Herdr, and the
// reason pane ids look like "w1:p3" — an id says which workspace it lives in.
type Workspace struct {
	ID   string
	Name string
	Cwd  string
	// Branch is the git branch of Cwd, shown under the name in the sidebar.
	Branch string
	// Named records that someone chose this workspace's name. An unnamed
	// workspace takes its name from the directory its focused pane is in, and
	// follows it: `cd` somewhere else and the sidebar says so. A workspace you
	// named stays named — that choice outranks anything inferred.
	Named bool

	tabs      []*Tab
	activeTab string
	nextTab   int
	nextPane  int
}

// Tab is one layout of panes inside a workspace.
type Tab struct {
	ID     string
	Name   string
	layout *Layout
	focus  string
	zoom   bool
}

func (w *Workspace) tab(id string) *Tab {
	for _, t := range w.tabs {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (w *Workspace) active() *Tab { return w.tab(w.activeTab) }

func (w *Workspace) newTabID() string {
	w.nextTab++
	return w.ID + ":t" + strconv.Itoa(w.nextTab)
}

func (w *Workspace) newPaneID() string {
	w.nextPane++
	return w.ID + ":p" + strconv.Itoa(w.nextPane)
}

func (w *Workspace) addTab(name string) *Tab {
	t := &Tab{ID: w.newTabID(), Name: name}
	w.tabs = append(w.tabs, t)
	w.activeTab = t.ID
	return t
}

func (w *Workspace) removeTab(id string) {
	for i, t := range w.tabs {
		if t.ID != id {
			continue
		}
		w.tabs = append(w.tabs[:i], w.tabs[i+1:]...)
		if w.activeTab == id {
			w.activeTab = ""
			if len(w.tabs) > 0 {
				w.activeTab = w.tabs[min(i, len(w.tabs)-1)].ID
			}
		}
		return
	}
}

// panes lists every pane in the workspace, tab order then layout order.
func (w *Workspace) panes() []string {
	var out []string
	for _, t := range w.tabs {
		out = append(out, t.layout.Panes()...)
	}
	return out
}

// displayName falls back to the directory name, which is what makes "one
// workspace per repo" readable without anyone naming anything.
func (w *Workspace) displayName() string {
	if w.Name != "" {
		return w.Name
	}
	if w.Cwd != "" {
		return prettyDir(w.Cwd)
	}
	return w.ID
}

// prettyDir names a directory the way a prompt would: its basename, except at
// the top of a home directory or the filesystem where the basename is
// meaningless on its own.
func prettyDir(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && dir == home {
		return "~"
	}
	if dir == "/" {
		return "/"
	}
	return filepath.Base(dir)
}

// setCwd moves a workspace to a directory, refreshing the branch with it.
// Reports whether anything changed.
func (w *Workspace) setCwd(dir string) bool {
	if dir == "" || dir == w.Cwd {
		return false
	}
	w.Cwd = dir
	branch := gitBranch(dir)
	if branch == "" {
		branch = gitBranchSlow(dir)
	}
	w.Branch = branch
	return true
}

// gitBranch reads the current branch of a checkout, or "" if the directory
// isn't one. Read from .git directly rather than by running git: this is
// called when workspaces are created and on a slow refresh, and shelling out
// per workspace is a lot of process spawning for one line of text.
func gitBranch(dir string) string {
	if dir == "" {
		return ""
	}
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return gitBranchFromParent(dir)
	}

	headFile := filepath.Join(gitPath, "HEAD")
	if !info.IsDir() {
		// A worktree or submodule: .git is a file pointing at the real dir.
		data, err := os.ReadFile(gitPath)
		if err != nil {
			return ""
		}
		target := strings.TrimSpace(strings.TrimPrefix(string(data), "gitdir:"))
		if target == "" {
			return ""
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		headFile = filepath.Join(target, "HEAD")
	}

	head, err := os.ReadFile(headFile)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(head))
	if ref, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
		return ref
	}
	if len(line) > 8 { // detached HEAD: show a short sha
		return line[:8]
	}
	return ""
}

// gitBranchFromParent walks up looking for a checkout, so a workspace opened
// in a subdirectory still shows its branch.
func gitBranchFromParent(dir string) string {
	parent := filepath.Dir(dir)
	if parent == dir || parent == "/" || parent == "." {
		return ""
	}
	if _, err := os.Stat(filepath.Join(parent, ".git")); err == nil {
		return gitBranch(parent)
	}
	return gitBranchFromParent(parent)
}

// gitBranchSlow is the fallback for exotic setups (worktrees with unusual
// layouts) where reading HEAD didn't produce anything.
func gitBranchSlow(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
