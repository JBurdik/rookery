package agentstatus

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestBuiltinManifestsLoad(t *testing.T) {
	r, errs := Load("")
	if len(errs) > 0 {
		t.Fatalf("built-in manifests should load cleanly: %v", errs)
	}
	for _, want := range []string{"claude", "codex", "gemini", "opencode", "aider"} {
		if !slices.Contains(r.Agents(), want) {
			t.Errorf("agent %q missing from %v", want, r.Agents())
		}
	}
	if len(r.generic) == 0 {
		t.Error("the generic manifest contributed no rules")
	}
	// The generic rules must be live for an agent with none of its own.
	if got := r.Evaluate("codex", Input{Bottom: []string{"(esc to interrupt)"}}); got != Working {
		t.Errorf("generic rule did not apply to codex: got %q", got)
	}
}

func TestUserManifestOverridesBuiltin(t *testing.T) {
	dir := t.TempDir()
	// Replace claude's rules with one that calls its idle screen blocked, so
	// the override is unmistakable.
	custom := `{
	  "id": "claude",
	  "exec": ["claude"],
	  "rules": [
	    {"id":"custom","state":"blocked","priority":500,"region":"bottom","contains":["? for shortcuts"]}
	  ]
	}`
	if err := os.WriteFile(filepath.Join(dir, "claude.json"), []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}

	r, errs := Load(dir)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	in := Input{Title: "claude", Bottom: []string{"? for shortcuts"}}
	if got := r.Evaluate("claude", in); got != Blocked {
		t.Errorf("user manifest did not override the built-in: got %q, want blocked", got)
	}
}

func TestUserManifestAddsNewAgent(t *testing.T) {
	dir := t.TempDir()
	custom := `{
	  "id": "mycoder",
	  "exec": ["mycoder", "mc"],
	  "rules": [
	    {"id":"busy","state":"working","priority":100,"region":"bottom","contains":["crunching"]}
	  ]
	}`
	if err := os.WriteFile(filepath.Join(dir, "mycoder.json"), []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}

	r, _ := Load(dir)
	if got := r.Agent("/usr/local/bin/mc", nil); got != "mycoder" {
		t.Errorf("Agent() = %q, want mycoder (matched by its exec alias)", got)
	}
	if got := r.Evaluate("mycoder", Input{Bottom: []string{"crunching numbers"}}); got != Working {
		t.Errorf("custom rule = %q, want working", got)
	}
}

// TestBadManifestIsSkippedNotFatal: one stray comma should cost that file's
// rules, not the daemon's ability to start.
func TestBadManifestIsSkippedNotFatal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "badregex.json"), []byte(
		`{"id":"x","rules":[{"id":"r","state":"working","regex":"([unclosed"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	r, errs := Load(dir)
	if len(errs) != 2 {
		t.Errorf("got %d errors, want 2 (one per bad file): %v", len(errs), errs)
	}
	if !slices.Contains(r.Agents(), "claude") {
		t.Error("a bad user file broke the built-in manifests")
	}
}

func TestEmptyRuleNeverMatches(t *testing.T) {
	// A rule with no regex, contains or any would otherwise match every
	// screen and pin the pane to one state forever.
	r := &Registry{byID: map[string]*Manifest{}}
	if err := r.add([]byte(`{"id":"x","rules":[{"id":"empty","state":"blocked","priority":9}]}`)); err != nil {
		t.Fatal(err)
	}
	r.index()
	if got := r.Evaluate("x", Input{Bottom: []string{"anything"}}); got != Unknown {
		t.Errorf("an empty rule matched: got %q", got)
	}
}

func TestWriteDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")
	written, err := WriteDefaults(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) == 0 {
		t.Fatal("nothing written")
	}
	// Running again must not clobber an edited file.
	again, err := WriteDefaults(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Errorf("second run rewrote %d files; existing files must be left alone", len(again))
	}
}
