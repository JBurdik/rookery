package agentstatus

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Built-in manifests. Embedding them means rookery recognises the common
// agents out of the box with nothing on disk, while still letting a user drop
// their own file in to add an agent or fix a rule without rebuilding.
//
// These are JSON rather than Herdr's TOML for one reason: rookery's other
// config files are JSON, and encoding/json is in the standard library. A TOML
// dependency to parse one directory of rule files is not a trade worth making.
//
//go:embed manifests/*.json
var builtinManifests embed.FS

// Region is the part of a pane a rule looks at. Beyond the two fixed regions,
// "bottom_non_empty_lines(N)" takes an N and returns the last N non-empty
// screen lines (plus any blank lines between them) — Herdr's region of the
// same name.
const (
	RegionTitle          = "title"                      // the terminal title the program set
	RegionOSCTitle       = "osc_title"                  // same source as title; Herdr's name for it
	RegionBottom         = "bottom"                     // the last 6 non-empty screen lines (fixed)
	RegionWholeRecent    = "whole_recent"               // the whole visible screen
	RegionAfterRule      = "after_last_horizontal_rule" // screen content after the last ─── line
	bottomNonEmptyPrefix = "bottom_non_empty_lines("
)

// Rule is one prioritised match. The highest-priority matching rule across
// all applicable manifests decides the state.
type Rule struct {
	ID       string   `json:"id"`
	State    State    `json:"state"`
	Priority int      `json:"priority"`
	Region   string   `json:"region"`
	Regex    string   `json:"regex,omitempty"`
	Contains []string `json:"contains,omitempty"`
	Any      []string `json:"any,omitempty"`
	// SkipStateUpdate marks a rule that suppresses a verdict rather than
	// producing one — Herdr uses this for transcript viewers layered over an
	// agent's own screen, where a match means "ignore this tick", not "the
	// agent is unknown". Only valid on a rule whose state is "unknown".
	SkipStateUpdate bool `json:"skip_state_update,omitempty"`
	// VisibleBlocker marks a blocked verdict as directly visible on screen
	// (a confirmation dialog), as opposed to inferred from an absence of
	// other markers. Mutually exclusive with SkipStateUpdate.
	VisibleBlocker bool `json:"visible_blocker,omitempty"`
	// Why is documentation for whoever reads the file next; unused at runtime.
	Why string `json:"why,omitempty"`

	re *regexp.Regexp
}

// Manifest is one agent's identity and rules.
type Manifest struct {
	ID          string   `json:"id"`
	Aliases     []string `json:"aliases,omitempty"`
	Exec        []string `json:"exec,omitempty"`
	Description string   `json:"description,omitempty"`
	Rules       []Rule   `json:"rules"`
}

// genericID is the manifest whose rules apply to every agent.
const genericID = "generic"

// Registry is the loaded set of manifests.
type Registry struct {
	byID    map[string]*Manifest
	byExec  map[string]string // executable name -> agent id
	generic []Rule
}

// Load builds a registry from the built-in manifests, then overlays any
// *.json in dir. A file with the same id as a built-in replaces it outright,
// so a user can correct a rule rather than fight it.
//
// A malformed user file is reported and skipped: one bad file should cost you
// that agent's rules, not the daemon's ability to start.
func Load(dir string) (*Registry, []error) {
	r := &Registry{byID: map[string]*Manifest{}, byExec: map[string]string{}}
	var errs []error

	entries, err := builtinManifests.ReadDir("manifests")
	if err != nil {
		return r, []error{err}
	}
	for _, e := range entries {
		data, err := builtinManifests.ReadFile("manifests/" + e.Name())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := r.add(data); err != nil {
			errs = append(errs, fmt.Errorf("built-in %s: %w", e.Name(), err))
		}
	}

	if dir != "" {
		userFiles, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		for _, path := range userFiles {
			data, err := os.ReadFile(path)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
				continue
			}
			if err := r.add(data); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
			}
		}
	}

	r.index()
	return r, errs
}

func (r *Registry) add(data []byte) error {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if m.ID == "" {
		return fmt.Errorf(`manifest has no "id"`)
	}
	for i := range m.Rules {
		rule := &m.Rules[i]
		if rule.State == "" {
			return fmt.Errorf("rule %q has no state", rule.ID)
		}
		if rule.Region == "" {
			rule.Region = RegionBottom
		}
		if !validRegion(rule.Region) {
			return fmt.Errorf("rule %q: invalid region %q", rule.ID, rule.Region)
		}
		if rule.SkipStateUpdate {
			if rule.State != Unknown {
				return fmt.Errorf("rule %q: skip_state_update requires state \"unknown\"", rule.ID)
			}
			if rule.VisibleBlocker {
				return fmt.Errorf("rule %q: skip_state_update cannot be combined with visible_blocker", rule.ID)
			}
		}
		if rule.Regex != "" {
			re, err := regexp.Compile(rule.Regex)
			if err != nil {
				return fmt.Errorf("rule %q: bad regex: %w", rule.ID, err)
			}
			rule.re = re
		}
		for j, c := range rule.Contains {
			rule.Contains[j] = strings.ToLower(c)
		}
		for j, c := range rule.Any {
			rule.Any[j] = strings.ToLower(c)
		}
	}
	r.byID[m.ID] = &m
	return nil
}

func (r *Registry) index() {
	r.byExec = map[string]string{}
	for id, m := range r.byID {
		if id == genericID {
			r.generic = m.Rules
			continue
		}
		for _, name := range append(append([]string{}, m.Exec...), m.Aliases...) {
			r.byExec[strings.ToLower(name)] = id
		}
		// The id itself is always a valid executable name to match on.
		r.byExec[strings.ToLower(id)] = id
	}
}

// Agent identifies which agent a command line is, by executable basename.
// Returns "" for anything unrecognised — a shell, a test runner, a server.
func (r *Registry) Agent(cmd string, args []string) string {
	if id, ok := r.byExec[executableName(cmd)]; ok {
		return id
	}
	// Agents are routinely not argv[0]: `npx claude`, `uv run aider`, and
	// anything installed as a script runs as `node /path/to/claude`.
	for _, a := range args {
		if id, ok := r.byExec[executableName(a)]; ok {
			return id
		}
	}
	return ""
}

// Agents lists the known agent ids, sorted.
func (r *Registry) Agents() []string {
	out := make([]string, 0, len(r.byID))
	for id := range r.byID {
		if id != genericID {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// Manifest returns one agent's manifest, or nil.
func (r *Registry) Manifest(id string) *Manifest { return r.byID[id] }

// Verdict is the result of evaluating a rule set: not just the state, but
// whether the matched rule wants it applied at all.
type Verdict struct {
	State State
	// Rule identifies the matching rule that produced State. It is empty when
	// no rule matched (and an agent therefore defaulted to Idle).
	Rule RuleMatch
	// SkipStateUpdate reports that the winning rule matched but asked for its
	// verdict to be discarded — the caller should keep whatever state it had
	// before this tick rather than treat State (always Unknown here) as a
	// verdict.
	SkipStateUpdate bool
	// VisibleBlocker reports that the winning rule is a Blocked verdict the
	// rule author marked as directly visible on screen.
	VisibleBlocker bool
}

// RuleMatch identifies the manifest rule behind a verdict. Keeping this
// small, exported provenance alongside Verdict lets callers explain a status
// without needing to rerun the detector or expose compiled regular expressions.
type RuleMatch struct {
	ID       string
	Source   string
	Priority int
	Region   string
}

// Evaluate returns the highest-priority state matching the input, considering
// the named agent's own rules plus the shared generic ones. Reports Unknown
// when nothing matches.
func (r *Registry) Evaluate(agent string, in Input) State {
	return r.EvaluateVerdict(agent, in).State
}

// EvaluateVerdict is Evaluate plus the winning rule's skip/visible flags.
func (r *Registry) EvaluateVerdict(agent string, in Input) Verdict {
	best := Verdict{State: Unknown}
	bestPriority := -1
	consider := func(source string, rules []Rule) {
		for _, rule := range rules {
			if rule.Priority <= bestPriority {
				continue
			}
			if rule.matches(regionText(rule.Region, in)) {
				best = Verdict{
					State:           rule.State,
					Rule:            RuleMatch{ID: rule.ID, Source: source, Priority: rule.Priority, Region: rule.Region},
					SkipStateUpdate: rule.SkipStateUpdate,
					VisibleBlocker:  rule.VisibleBlocker && rule.State == Blocked,
				}
				bestPriority = rule.Priority
			}
		}
	}
	if m := r.byID[agent]; m != nil {
		consider(agent, m.Rules)
	}
	consider(genericID, r.generic)
	return best
}

// EvaluateAgent is Evaluate for a pane known to hold an agent: it never
// reports Unknown, because for a recognised agent "no working or blocked
// marker on screen" means it is sitting at its prompt.
//
// The caller must not fall back to output activity here — agents repaint
// continuously (spinners, context meters, token counters), so "printed
// something recently" stays true while one sits idle for minutes.
func (r *Registry) EvaluateAgent(agent string, in Input) State {
	v := r.EvaluateAgentVerdict(agent, in)
	return v.State
}

// EvaluateAgentVerdict is EvaluateAgent plus the winning rule's skip/visible
// flags. A SkipStateUpdate verdict is left as Unknown rather than defaulted
// to Idle — the caller is expected to keep its previous state instead.
func (r *Registry) EvaluateAgentVerdict(agent string, in Input) Verdict {
	v := r.EvaluateVerdict(agent, in)
	if v.State == Unknown && !v.SkipStateUpdate {
		v.State = Idle
	}
	return v
}

// regionText resolves a rule's region against the input, returning the text
// a rule's matchers run against. Title regions keep their original case (the
// spinner regexp needs the untouched glyphs); everything else is lowercased,
// matching the "bottom" region's long-standing behaviour.
func regionText(region string, in Input) string {
	switch {
	case region == RegionTitle || region == RegionOSCTitle:
		return strings.TrimSpace(in.Title)
	case region == RegionBottom:
		return strings.ToLower(strings.Join(in.Bottom, "\n"))
	case region == RegionWholeRecent:
		return strings.ToLower(strings.Join(in.Screen, "\n"))
	case region == RegionAfterRule:
		return strings.ToLower(strings.Join(afterLastHorizontalRule(in.Screen), "\n"))
	default:
		if n, ok := parseCountRegion(region); ok {
			return strings.ToLower(strings.Join(bottomNonEmptyLines(in.Screen, n), "\n"))
		}
		return ""
	}
}

// validRegion reports whether a manifest's region string is one this package
// knows how to resolve.
func validRegion(region string) bool {
	switch region {
	case RegionTitle, RegionOSCTitle, RegionBottom, RegionWholeRecent, RegionAfterRule:
		return true
	}
	_, ok := parseCountRegion(region)
	return ok
}

// parseCountRegion parses "bottom_non_empty_lines(N)", returning N.
func parseCountRegion(region string) (int, bool) {
	rest, ok := strings.CutPrefix(region, bottomNonEmptyPrefix)
	if !ok {
		return 0, false
	}
	rest, ok = strings.CutSuffix(rest, ")")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// afterLastHorizontalRule returns everything after the last line that is
// itself a horizontal rule (a run of ─, Claude Code's box-drawing style). If
// no rule line is found, it returns the whole screen — same fallback Herdr
// uses.
func afterLastHorizontalRule(lines []string) []string {
	last := -1
	for i, line := range lines {
		if isHorizontalRule(line) {
			last = i
		}
	}
	return lines[last+1:]
}

// isHorizontalRule reports whether a line is (up to surrounding whitespace)
// a run of box-drawing ─ characters — Claude Code's separator between the
// transcript and its prompt box.
func isHorizontalRule(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	runes := []rune(trimmed)
	n := 0
	for n < len(runes) && runes[n] == '─' {
		n++
	}
	if n == 0 {
		return false
	}
	rest := strings.TrimSpace(string(runes[n:]))
	return rest == "" || n >= 3
}

// bottomNonEmptyLines returns the last count non-empty lines, plus any blank
// lines between them, working back from the end of the screen.
func bottomNonEmptyLines(lines []string, count int) []string {
	idx := -1
	found := 0
	for i := len(lines) - 1; i >= 0 && found < count; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			idx = i
			found++
		}
	}
	if idx == -1 {
		return nil
	}
	return lines[idx:]
}

func (rule Rule) matches(hay string) bool {
	if rule.re != nil && !rule.re.MatchString(hay) {
		return false
	}
	for _, s := range rule.Contains {
		if !strings.Contains(hay, s) {
			return false
		}
	}
	if len(rule.Any) > 0 {
		found := false
		for _, s := range rule.Any {
			if strings.Contains(hay, s) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return rule.re != nil || len(rule.Contains) > 0 || len(rule.Any) > 0
}

// WriteDefaults copies the built-in manifests into dir so they can be edited.
// Existing files are left alone — this is a starting point, not a reset.
func WriteDefaults(dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	entries, err := builtinManifests.ReadDir("manifests")
	if err != nil {
		return nil, err
	}
	var written []string
	for _, e := range entries {
		dst := filepath.Join(dir, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		data, err := builtinManifests.ReadFile("manifests/" + e.Name())
		if err != nil {
			return written, err
		}
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			return written, err
		}
		written = append(written, dst)
	}
	return written, nil
}

func executableName(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToLower(strings.TrimSuffix(s, ".exe"))
}
