package agentstatus

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

// Region is the part of a pane a rule looks at.
const (
	RegionTitle  = "title"  // the terminal title the program set
	RegionBottom = "bottom" // the last few non-empty screen lines
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

// Evaluate returns the highest-priority state matching the input, considering
// the named agent's own rules plus the shared generic ones. Reports Unknown
// when nothing matches.
func (r *Registry) Evaluate(agent string, in Input) State {
	title := strings.TrimSpace(in.Title)
	bottom := strings.ToLower(strings.Join(in.Bottom, "\n"))

	best, bestPriority := Unknown, -1
	consider := func(rules []Rule) {
		for _, rule := range rules {
			if rule.Priority <= bestPriority {
				continue
			}
			hay := bottom
			if rule.Region == RegionTitle {
				// The spinner regexp needs the untouched title: lowercasing
				// is fine for substrings, but the glyph ranges must match
				// as-is.
				hay = title
			}
			if rule.matches(hay) {
				best, bestPriority = rule.State, rule.Priority
			}
		}
	}
	if m := r.byID[agent]; m != nil {
		consider(m.Rules)
	}
	consider(r.generic)
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
	if s := r.Evaluate(agent, in); s != Unknown {
		return s
	}
	return Idle
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
