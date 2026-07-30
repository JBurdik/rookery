package state

import (
	"testing"

	"github.com/jirkab/rookery/internal/termgrid"
)

func TestPaneWantsMouseIgnoresStaleShellModes(t *testing.T) {
	grid := termgrid.New(80, 24)
	grid.Write([]byte("\x1b[?1000h\x1b[?1006h"))
	if !grid.MouseEnabled() {
		t.Fatal("test setup: grid should have mouse mode enabled")

		shell := &Pane{Cmd: "/bin/zsh", Running: "zsh", Grid: grid}
		if shell.wantsMouse() {
			t.Fatal("shell prompt must ignore mouse mode left by a previous program")
		}

		program := &Pane{Cmd: "/bin/zsh", Running: "claude", Grid: grid}
		if !program.wantsMouse() {
			t.Fatal("foreground program that requested mouse input should keep it")
		}
	}
}
