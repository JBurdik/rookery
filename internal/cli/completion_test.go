package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunCompletionPrintsSelectedScript(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			old := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			os.Stdout = w
			err = RunCompletion([]string{shell})
			w.Close()
			os.Stdout = old
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(got), "rook") {
				t.Fatalf("completion script does not mention rook: %q", got)
			}
		})
	}
}

func TestRunCompletionRejectsUnsupportedShell(t *testing.T) {
	if err := RunCompletion([]string{"fish"}); err == nil || !strings.Contains(err.Error(), "unknown shell") {
		t.Fatalf("RunCompletion(fish) error = %v, want unsupported-shell error", err)
	}
}
