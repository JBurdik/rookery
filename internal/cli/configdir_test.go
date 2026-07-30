package cli

import (
	"path/filepath"
	"testing"

	"github.com/jirkab/rookery/internal/integration"
)

func TestPiProjectTargetUsesPiExtensionDirectory(t *testing.T) {
	old, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	// resolve reads the process cwd. The test package runs there, so assert the
	// stable suffix rather than changing global process state in a parallel test.
	paths, err := (&targetFlags{project: true}).resolve(integration.Specs["pi"])
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(old, ".pi", "extensions", "rook-agent-state.ts")
	if len(paths) != 1 || paths[0] != want {
		t.Fatalf("Pi project target = %v, want %q", paths, want)
	}
}

func TestPiSkillTargetUsesAgentDirectory(t *testing.T) {
	paths, err := (&targetFlags{configDir: "/tmp/pi-agent"}).resolve(integration.Specs["pi"])
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/tmp/pi-agent", "extensions", "rook-agent-state.ts")
	if len(paths) != 1 || paths[0] != want {
		t.Fatalf("Pi config target = %v, want %q", paths, want)
	}
}
