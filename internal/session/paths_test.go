package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveDeletesSessionDirectory(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	name := "disposable"
	if err := EnsureDir(name); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LayoutPath(name), []byte("layout"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Remove(name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(BaseDir(), name)); !os.IsNotExist(err) {
		t.Fatalf("session directory still exists: %v", err)
	}
}

func TestRemoveRejectsPathTraversal(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := Remove("../outside"); err == nil {
		t.Fatal("Remove accepted a path traversal session name")
	}
}
