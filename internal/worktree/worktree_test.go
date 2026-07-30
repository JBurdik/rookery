package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newRepo builds a throwaway repository with one commit.
func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-qm", "init")
	return dir
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestCreateListRemove(t *testing.T) {
	repo := newRepo(t)
	mgr := New(filepath.Join(t.TempDir(), "worktrees"))

	tree, err := mgr.Create(repo, "fan1-1", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tree.Branch != "rook/fan1-1" {
		t.Errorf("branch = %q, want rook/fan1-1", tree.Branch)
	}
	if _, err := os.Stat(filepath.Join(tree.Path, "README.md")); err != nil {
		t.Errorf("worktree has no checkout: %v", err)
	}

	// Re-creating the same name reuses it rather than failing, so re-running a
	// fan-out lands you back where you were.
	again, err := mgr.Create(repo, "fan1-1", "")
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if again.Path != tree.Path {
		t.Errorf("second Create made a different path: %q vs %q", again.Path, tree.Path)
	}

	trees, err := mgr.List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(trees) != 1 || trees[0].Name != "fan1-1" {
		t.Fatalf("List = %+v, want one named fan1-1", trees)
	}
	// The repo's own working copy is a worktree too, and must not be listed —
	// it is not ours and certainly not ours to remove.
	for _, tr := range trees {
		if tr.Path == repo {
			t.Error("List included the main repository")
		}
	}

	if err := mgr.Remove(repo, "fan1-1", false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(tree.Path); !os.IsNotExist(err) {
		t.Error("worktree directory survived Remove")
	}
	if branches := mustGit(t, repo, "branch", "--list", "rook/*"); branches != "" {
		t.Errorf("branch survived Remove: %q", branches)
	}
}

// TestRemoveRefusesToDiscardWork is the safety property that matters: an
// agent's uncommitted output must not vanish because a cleanup ran.
func TestRemoveRefusesToDiscardWork(t *testing.T) {
	repo := newRepo(t)
	mgr := New(filepath.Join(t.TempDir(), "worktrees"))

	tree, err := mgr.Create(repo, "work", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree.Path, "README.md"), []byte("agent edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Remove(repo, "work", false); err == nil {
		t.Fatal("Remove discarded uncommitted work without --force")
	}
	if _, err := os.Stat(tree.Path); err != nil {
		t.Error("the refused Remove deleted the worktree anyway")
	}
	if err := mgr.Remove(repo, "work", true); err != nil {
		t.Errorf("forced Remove: %v", err)
	}
}

// TestDiffstatCountsUntracked covers the case that read as "this agent did
// nothing": its entire contribution was a new file, which `git diff` ignores.
func TestDiffstatCountsUntracked(t *testing.T) {
	repo := newRepo(t)
	mgr := New(filepath.Join(t.TempDir(), "worktrees"))
	tree, err := mgr.Create(repo, "d", "")
	if err != nil {
		t.Fatal(err)
	}

	if got := Diffstat(tree.Path); got != "" {
		t.Errorf("fresh worktree diffstat = %q, want empty", got)
	}

	if err := os.WriteFile(filepath.Join(tree.Path, "answer.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Diffstat(tree.Path); !strings.Contains(got, "new file") {
		t.Errorf("untracked file not reported: %q", got)
	}

	// A tracked edit shows up as a shortstat.
	if err := os.WriteFile(filepath.Join(tree.Path, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Diffstat(tree.Path)
	if !strings.Contains(got, "1 file changed") || !strings.Contains(got, "new file") {
		t.Errorf("diffstat = %q, want both the edit and the new file", got)
	}
}

func TestSanitise(t *testing.T) {
	tests := []struct{ in, want string }{
		{"fan1-1", "fan1-1"},
		{"feature/thing", "feature-thing"},
		{"a b c", "a-b-c"},
		{"--weird--", "weird"},
	}
	for _, tt := range tests {
		if got := sanitise(tt.in); got != tt.want {
			t.Errorf("sanitise(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRepoRootRejectsNonRepo(t *testing.T) {
	if _, err := RepoRoot(t.TempDir()); err == nil {
		t.Error("RepoRoot accepted a directory that is not a repository")
	}
}

func TestReviewAndPromote(t *testing.T) {
	repo := newRepo(t)
	mgr := New(filepath.Join(t.TempDir(), "worktrees"))
	tree, err := mgr.Create(repo, "winner", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree.Path, "README.md"), []byte("winner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, tree.Path, "add", "README.md")
	mustGit(t, tree.Path, "commit", "-qm", "make it win")

	review, err := ReviewBranch(repo, tree.Branch, tree.Path, true)
	if err != nil {
		t.Fatalf("ReviewBranch: %v", err)
	}
	if len(review.Commits) != 1 || !strings.Contains(review.Commits[0], "make it win") {
		t.Errorf("commits = %q", review.Commits)
	}
	if len(review.Files) != 1 || review.Files[0] != "M\tREADME.md" {
		t.Errorf("files = %q", review.Files)
	}
	if !strings.Contains(review.Patch, "winner") {
		t.Error("review patch does not contain candidate change")
	}

	if err := Promote(repo, tree); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if got := mustGit(t, repo, "log", "-1", "--format=%s"); got != "make it win" {
		t.Errorf("source HEAD = %q", got)
	}
}

func TestPromoteRefusesDirtyCandidate(t *testing.T) {
	repo := newRepo(t)
	mgr := New(filepath.Join(t.TempDir(), "worktrees"))
	tree, err := mgr.Create(repo, "dirty", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree.Path, "answer.txt"), []byte("not committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Promote(repo, tree); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("Promote error = %v, want dirty candidate refusal", err)
	}
}
