// Package worktree manages the git worktrees a fan-out runs in.
//
// Each agent gets its own checkout and its own branch, so three agents
// attacking the same task cannot fight over the index, and comparing their
// answers is `git diff` rather than reading three transcripts.
//
// Worktrees live under rookery's state directory, not beside the repo: a
// fan-out is scratch space, and scattering sibling directories through
// someone's source tree is a rude thing for a multiplexer to do.
package worktree

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Tree is one worktree rookery created.
type Tree struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Repo   string `json:"repo"`
	// Head is the short sha the branch points at.
	Head string `json:"head,omitempty"`
	// Dirty reports uncommitted changes — for a fan-out, that means the agent
	// did something but has not committed it.
	Dirty bool `json:"dirty"`
	// Ahead is how many commits this branch has that its base does not.
	Ahead int `json:"ahead"`
}

// Manager creates and removes worktrees under a root directory.
type Manager struct {
	Root string // where worktrees are created
}

func New(root string) *Manager { return &Manager{Root: root} }

// ownsPath reports whether a worktree is one of ours.
//
// Both sides are resolved before comparing, because git reports the real path
// while our root may be reached through a symlink — on macOS /var is a link to
// /private/var, which made List quietly return nothing and left `fan clean`
// unable to remove anything it had created.
func (m *Manager) ownsPath(path string) bool {
	return strings.HasPrefix(resolve(path), resolve(m.Root))
}

func resolve(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

// RepoRoot returns the top level of the repository containing dir, or an error
// if it isn't one.
func RepoRoot(dir string) (string, error) {
	out, err := run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not a git repository", dir)
	}
	return out, nil
}

// Create adds a worktree for repo on a new branch. The branch is created from
// base, or from the repository's current HEAD when base is empty.
//
// An existing worktree with the same name is reused rather than treated as an
// error: re-running a fan-out should land you back where you were, not fail.
func (m *Manager) Create(repo, name, base string) (Tree, error) {
	repo, err := RepoRoot(repo)
	if err != nil {
		return Tree{}, err
	}
	if name == "" {
		return Tree{}, errors.New("worktree name is required")
	}

	branch := "rook/" + name
	path := filepath.Join(m.Root, filepath.Base(repo)+"-"+sanitise(name))

	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return m.inspect(repo, name, path, branch)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Tree{}, err
	}

	args := []string{"worktree", "add", "-b", branch, path}
	if base != "" {
		args = append(args, base)
	}
	if _, err := run(repo, args...); err != nil {
		// A leftover branch from a previous run is the common cause; reuse it
		// rather than making the user clean up by hand.
		if _, retry := run(repo, "worktree", "add", path, branch); retry != nil {
			return Tree{}, fmt.Errorf("git worktree add: %w", err)
		}
	}
	return m.inspect(repo, name, path, branch)
}

// List returns the worktrees rookery created for a repo.
func (m *Manager) List(repo string) ([]Tree, error) {
	repo, err := RepoRoot(repo)
	if err != nil {
		return nil, err
	}
	out, err := run(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	var trees []Tree
	var cur Tree
	flush := func() {
		// Only report the ones under our root: the user's own worktrees are
		// none of our business, and certainly not ours to remove.
		if cur.Path != "" && m.ownsPath(cur.Path) {
			cur.Repo = repo
			cur.Name = strings.TrimPrefix(cur.Branch, "rook/")
			trees = append(trees, cur)
		}
		cur = Tree{}
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = shortSha(strings.TrimPrefix(line, "HEAD "))
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()

	for i := range trees {
		trees[i].Dirty = isDirty(trees[i].Path)
		trees[i].Ahead = aheadOf(trees[i].Path, repo)
	}
	return trees, nil
}

// Remove deletes a worktree and its branch. force discards uncommitted work,
// which is why it has to be asked for explicitly.
func (m *Manager) Remove(repo, name string, force bool) error {
	repo, err := RepoRoot(repo)
	if err != nil {
		return err
	}
	trees, err := m.List(repo)
	if err != nil {
		return err
	}
	for _, t := range trees {
		if t.Name != name {
			continue
		}
		if t.Dirty && !force {
			return fmt.Errorf("worktree %q has uncommitted changes; pass --force to discard them", name)
		}
		args := []string{"worktree", "remove", t.Path}
		if force {
			args = append(args, "--force")
		}
		if _, err := run(repo, args...); err != nil {
			return fmt.Errorf("git worktree remove: %w", err)
		}
		// The branch outlives the worktree unless asked to go too. Deleting it
		// is safe here because rookery created it and prefixed it "rook/".
		if strings.HasPrefix(t.Branch, "rook/") {
			flag := "-d"
			if force {
				flag = "-D"
			}
			_, _ = run(repo, "branch", flag, t.Branch)
		}
		return nil
	}
	return fmt.Errorf("no rookery worktree named %q", name)
}

func (m *Manager) inspect(repo, name, path, branch string) (Tree, error) {
	t := Tree{Name: name, Path: path, Branch: branch, Repo: repo}
	if head, err := run(path, "rev-parse", "--short", "HEAD"); err == nil {
		t.Head = head
	}
	t.Dirty = isDirty(path)
	t.Ahead = aheadOf(path, repo)
	return t, nil
}

// Diffstat summarises what an agent actually changed, committed or not.
//
// Untracked files are counted separately and deliberately: `git diff` ignores
// them entirely, so an agent whose whole contribution is a new file looked like
// it had done nothing at all.
func Diffstat(path string) string {
	var parts []string
	if out, err := run(path, "diff", "--shortstat", "HEAD"); err == nil && out != "" {
		parts = append(parts, out)
	}
	if out, err := run(path, "ls-files", "--others", "--exclude-standard"); err == nil {
		if n := len(nonEmptyLines(out)); n > 0 {
			parts = append(parts, fmt.Sprintf("%d new file(s)", n))
		}
	}
	return strings.Join(parts, " · ")
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func isDirty(path string) bool {
	out, err := run(path, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != ""
}

// aheadOf counts commits on the worktree's HEAD that the repo's HEAD lacks.
func aheadOf(path, repo string) int {
	base, err := run(repo, "rev-parse", "HEAD")
	if err != nil {
		return 0
	}
	out, err := run(path, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return 0
	}
	n := 0
	_, _ = fmt.Sscanf(out, "%d", &n)
	return n
}

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func shortSha(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// sanitise makes a name safe for a directory and a branch.
func sanitise(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
