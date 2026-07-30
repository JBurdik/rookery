// Setup is the interactive wizard behind `rook setup`: pick which agents to
// wire up, see exactly what that means, then install it. It exists because
// `rook skill install` and `rook integration install <agent>` are two
// commands with flags to look up, and most people just want "make my agents
// report their own status" without reading `--config-dir` first.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jirkab/rookery/internal/integration"
	"github.com/jirkab/rookery/internal/skill"
)

var (
	setupTitle    = lipgloss.NewStyle().Bold(true)
	setupMuted    = lipgloss.NewStyle().Faint(true)
	setupSelected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	setupGood     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	setupBad      = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

// setupAgent is one row of the wizard: an agent Herdr-style integration knows
// about, plus what's currently on disk for it.
type setupAgent struct {
	id   string
	spec integration.Spec

	found        bool   // on $PATH
	skillPath    string // where `rook skill --install` would write
	hasSkill     bool
	hooksPath    string // where `rook integration install` would write
	hasHooks     bool
	targetErr    error // set if the agent's config directory can't be resolved
	selected     bool
	installErr   error
	installedNow bool
}

type setupStep int

const (
	stepPick setupStep = iota
	stepConfirm
	stepDone
)

type setupModel struct {
	agents  []setupAgent
	cursor  int
	step    setupStep
	rookBin string
	err     error
}

// RunSetupWizard drives `rook setup` end to end: builds the agent list from
// what's installed and found on PATH, then hands control to bubbletea.
func RunSetupWizard() error {
	bin := "rook"
	if exe, err := os.Executable(); err == nil {
		bin = exe
	}
	m := &setupModel{rookBin: bin, agents: buildSetupAgents()}
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return err
	}
	if fm, ok := final.(*setupModel); ok && fm.err != nil {
		return fm.err
	}
	return nil
}

func buildSetupAgents() []setupAgent {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	out := make([]setupAgent, 0, len(integration.IDs()))
	for _, id := range integration.IDs() {
		spec := integration.Specs[id]
		_, lookErr := exec.LookPath(id)
		a := setupAgent{id: id, spec: spec, found: lookErr == nil}

		dirs := spec.ConfigDirs(home, cwd)
		if len(dirs) == 0 {
			a.targetErr = fmt.Errorf("no %s configuration directory found", spec.Name)
			out = append(out, a)
			continue
		}
		dir := dirs[0]
		a.hooksPath = spec.SettingsIn(dir)
		a.skillPath = skill.PathIn(dir)
		if st, err := integration.StatusOf(id, a.hooksPath); err == nil {
			a.hasHooks = st.Installed
		}
		if _, err := os.Stat(a.skillPath); err == nil {
			a.hasSkill = true
		}
		a.selected = a.found && !(a.hasSkill && a.hasHooks)
		out = append(out, a)
	}
	return out
}

func (m *setupModel) Init() tea.Cmd { return nil }

func (m *setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch m.step {
	case stepPick:
		return m.updatePick(keyMsg)
	case stepConfirm:
		return m.updateConfirm(keyMsg)
	default: // stepDone
		return m, tea.Quit
	}
}

func (m *setupModel) updatePick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc", "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.agents)-1 {
			m.cursor++
		}
	case " ":
		if m.cursor < len(m.agents) && m.agents[m.cursor].targetErr == nil {
			m.agents[m.cursor].selected = !m.agents[m.cursor].selected
		}
	case "enter":
		if m.anySelected() {
			m.step = stepConfirm
		}
	}
	return m, nil
}

func (m *setupModel) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc", "q":
		return m, tea.Quit
	case "b":
		m.step = stepPick
	case "enter", "y":
		m.install()
		m.step = stepDone
	}
	return m, nil
}

func (m *setupModel) anySelected() bool {
	for _, a := range m.agents {
		if a.selected {
			return true
		}
	}
	return false
}

// install runs the same code path `rook skill --install` and `rook
// integration install` use — the wizard only decides which agents and
// prints the outcome, it never reimplements what gets written.
func (m *setupModel) install() {
	for i := range m.agents {
		a := &m.agents[i]
		if !a.selected {
			continue
		}
		if err := skill.Install(a.skillPath); err != nil {
			a.installErr = err
			continue
		}
		if _, err := integration.Install(a.id, a.hooksPath, m.rookBin); err != nil {
			a.installErr = err
			continue
		}
		a.installedNow = true
	}
}

func (m *setupModel) View() string {
	switch m.step {
	case stepPick:
		return m.viewPick()
	case stepConfirm:
		return m.viewConfirm()
	default:
		return m.viewDone()
	}
}

func (m *setupModel) viewPick() string {
	var b strings.Builder
	b.WriteString(setupTitle.Render("rook setup — teach agents to drive rookery"))
	b.WriteString("\n")
	b.WriteString(setupMuted.Render("space to toggle, enter to continue, q to quit"))
	b.WriteString("\n\n")

	for i, a := range m.agents {
		cursor := "  "
		if i == m.cursor {
			cursor = "❯ "
		}
		box := "[ ]"
		if a.selected {
			box = "[x]"
		}
		line := fmt.Sprintf("%s%s %s", cursor, box, a.spec.Name)
		if i == m.cursor {
			line = setupSelected.Render(line)
		}
		b.WriteString(line)
		b.WriteString("  ")
		b.WriteString(setupMuted.Render(agentStatusLine(a)))
		b.WriteString("\n")
	}
	return b.String()
}

func agentStatusLine(a setupAgent) string {
	if a.targetErr != nil {
		return "not found: " + a.targetErr.Error()
	}
	var bits []string
	if a.found {
		bits = append(bits, "on PATH")
	} else {
		bits = append(bits, "not on PATH")
	}
	if a.hasSkill {
		bits = append(bits, "skill installed")
	}
	if a.hasHooks {
		bits = append(bits, "hooks installed")
	}
	return strings.Join(bits, ", ")
}

func (m *setupModel) viewConfirm() string {
	var b strings.Builder
	b.WriteString(setupTitle.Render("about to install"))
	b.WriteString("\n\n")
	for _, a := range m.agents {
		if !a.selected {
			continue
		}
		fmt.Fprintf(&b, "%s\n", a.spec.Name)
		fmt.Fprintf(&b, "  skill  -> %s\n", a.skillPath)
		fmt.Fprintf(&b, "  hooks  -> %s\n", a.hooksPath)
	}
	b.WriteString("\n")
	b.WriteString(setupMuted.Render("enter to install, b to go back, q to cancel"))
	return b.String()
}

func (m *setupModel) viewDone() string {
	var b strings.Builder
	b.WriteString(setupTitle.Render("done"))
	b.WriteString("\n\n")
	for _, a := range m.agents {
		if !a.selected {
			continue
		}
		switch {
		case a.installErr != nil:
			fmt.Fprintf(&b, "%s %s\n", setupBad.Render("✗"), a.spec.Name+": "+a.installErr.Error())
		case a.installedNow:
			fmt.Fprintf(&b, "%s %s\n", setupGood.Render("✓"), a.spec.Name)
		}
	}
	b.WriteString("\n")
	b.WriteString(setupMuted.Render("restart any running agents for hooks to load. press any key to exit."))
	return b.String()
}
