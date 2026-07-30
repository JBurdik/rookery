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

func TestRichRegions(t *testing.T) {
	screen := []string{
		"some old transcript",
		"",
		"────────────────────────",
		"❯ ",
		"",
		"crunching numbers",
		"almost done",
	}

	r := &Registry{byID: map[string]*Manifest{}}
	if err := r.add([]byte(`{"id":"x","rules":[
		{"id":"whole","state":"working","priority":10,"region":"whole_recent","contains":["old transcript"]},
		{"id":"after_rule","state":"blocked","priority":20,"region":"after_last_horizontal_rule","contains":["almost done"]},
		{"id":"bottom2","state":"idle","priority":5,"region":"bottom_non_empty_lines(2)","contains":["crunching"]}
	]}`)); err != nil {
		t.Fatal(err)
	}
	r.index()

	in := Input{Screen: screen}
	if got := r.Evaluate("x", in); got != Blocked {
		t.Errorf("Evaluate() = %q, want blocked (after_last_horizontal_rule + whole_recent both match, higher priority wins)", got)
	}

	// bottom_non_empty_lines(2) should see "crunching numbers" and "almost
	// done" (the last two non-empty lines), not "old transcript".
	in2 := Input{Screen: []string{"crunching numbers", "", "almost done"}}
	only := &Registry{byID: map[string]*Manifest{}}
	if err := only.add([]byte(`{"id":"x","rules":[
		{"id":"bottom2","state":"idle","priority":5,"region":"bottom_non_empty_lines(2)","contains":["crunching"]}
	]}`)); err != nil {
		t.Fatal(err)
	}
	only.index()
	if got := only.Evaluate("x", in2); got != Idle {
		t.Errorf("bottom_non_empty_lines(2) = %q, want idle", got)
	}
	in3 := Input{Screen: []string{"crunching numbers", "unrelated", "unrelated", "almost done"}}
	if got := only.Evaluate("x", in3); got != Unknown {
		t.Errorf("bottom_non_empty_lines(2) matched outside its window: got %q", got)
	}
}

func TestOSCTitleRegionMatchesTitleSource(t *testing.T) {
	r := &Registry{byID: map[string]*Manifest{}}
	if err := r.add([]byte(`{"id":"x","rules":[
		{"id":"r","state":"working","priority":10,"region":"osc_title","contains":["cooking"]}
	]}`)); err != nil {
		t.Fatal(err)
	}
	r.index()
	if got := r.Evaluate("x", Input{Title: "cooking dinner"}); got != Working {
		t.Errorf("osc_title region = %q, want working", got)
	}
}

func TestSkipStateUpdateSuppressesVerdict(t *testing.T) {
	r := &Registry{byID: map[string]*Manifest{}}
	if err := r.add([]byte(`{"id":"x","rules":[
		{"id":"viewer","state":"unknown","priority":100,"region":"bottom","contains":["(end)"],"skip_state_update":true},
		{"id":"busy","state":"working","priority":10,"region":"bottom","contains":["crunching"]}
	]}`)); err != nil {
		t.Fatal(err)
	}
	r.index()

	// The skip rule outranks "busy" and matches, so EvaluateAgentVerdict must
	// not default Unknown to Idle here.
	v := r.EvaluateAgentVerdict("x", Input{Bottom: []string{"crunching numbers", "(END)"}})
	if !v.SkipStateUpdate {
		t.Fatal("expected SkipStateUpdate to be set")
	}
	if v.State != Unknown {
		t.Errorf("skip verdict State = %q, want unknown (caller keeps prior state)", v.State)
	}

	// Without the viewer marker, the lower-priority "busy" rule should win
	// normally.
	if got := r.EvaluateAgent("x", Input{Bottom: []string{"crunching numbers"}}); got != Working {
		t.Errorf("EvaluateAgent() = %q, want working", got)
	}
}

func TestSkipStateUpdateValidation(t *testing.T) {
	r := &Registry{byID: map[string]*Manifest{}}
	if err := r.add([]byte(`{"id":"x","rules":[
		{"id":"bad","state":"working","priority":10,"region":"bottom","contains":["x"],"skip_state_update":true}
	]}`)); err == nil {
		t.Fatal("expected an error: skip_state_update requires state \"unknown\"")
	}

	if err := r.add([]byte(`{"id":"x","rules":[
		{"id":"bad","state":"unknown","priority":10,"region":"bottom","contains":["x"],"skip_state_update":true,"visible_blocker":true}
	]}`)); err == nil {
		t.Fatal("expected an error: skip_state_update cannot combine with visible_blocker")
	}
}

func TestVisibleBlockerReportedOnMatch(t *testing.T) {
	r := &Registry{byID: map[string]*Manifest{}}
	if err := r.add([]byte(`{"id":"x","rules":[
		{"id":"dialog","state":"blocked","priority":10,"region":"bottom","contains":["y/n"],"visible_blocker":true}
	]}`)); err != nil {
		t.Fatal(err)
	}
	r.index()

	v := r.EvaluateVerdict("x", Input{Bottom: []string{"proceed? (y/n)"}})
	if v.State != Blocked || !v.VisibleBlocker {
		t.Errorf("EvaluateVerdict() = %+v, want blocked with VisibleBlocker set", v)
	}
}

func TestVerdictIdentifiesWinningRule(t *testing.T) {
	r := &Registry{byID: map[string]*Manifest{}}
	if err := r.add([]byte(`{"id":"x","rules":[
		{"id":"agent-busy","state":"working","priority":10,"region":"bottom","contains":["working"]}
	]}`)); err != nil {
		t.Fatal(err)
	}
	if err := r.add([]byte(`{"id":"generic","rules":[
		{"id":"shared-blocker","state":"blocked","priority":20,"region":"bottom","contains":["continue?"]}
	]}`)); err != nil {
		t.Fatal(err)
	}
	r.index()

	v := r.EvaluateAgentVerdict("x", Input{Bottom: []string{"working", "continue?"}})
	if v.State != Blocked {
		t.Fatalf("State = %q, want blocked", v.State)
	}
	if got, want := v.Rule, (RuleMatch{ID: "shared-blocker", Source: "generic", Priority: 20, Region: RegionBottom}); got != want {
		t.Errorf("Rule = %#v, want %#v", got, want)
	}
}

func TestInvalidRegionRejected(t *testing.T) {
	r := &Registry{byID: map[string]*Manifest{}}
	err := r.add([]byte(`{"id":"x","rules":[
		{"id":"bad","state":"working","priority":10,"region":"nonsense","contains":["x"]}
	]}`))
	if err == nil {
		t.Fatal("expected an error for an unknown region name")
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
