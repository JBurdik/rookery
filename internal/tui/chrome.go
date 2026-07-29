package tui

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/jirkab/rookery/internal/attachproto"
	"github.com/jirkab/rookery/internal/config"
)

// The client's chrome: a sidebar of panels on the left, a numbered tab strip
// along the top, and a status line that only appears when it has something to
// say. Modelled on Herdr's layout — the sidebar is where you read state, and
// the terminal itself keeps almost all of the screen.
const (
	headerRows = 1
	// managerRows is the always-visible manager bar. It costs one row of pane
	// forever, which is the point: the manager is the thing you talk to most,
	// and a bar that only exists while you type at it made its answer look
	// like a status message that had already scrolled away.
	managerRows = 1
	statusRows  = 1
	chromeRows  = headerRows + managerRows + statusRows
	minPaneCols = 20
)

// palette holds every style the chrome draws with, built once from the
// user's colours so a theme change is a config edit rather than a hunt
// through render code.
type palette struct {
	sidebarBG      lipgloss.Style
	sidebarHeading lipgloss.Style
	sidebarItem    lipgloss.Style
	sidebarMuted   lipgloss.Style
	sidebarActive  lipgloss.Style
	sidebarCursor  lipgloss.Style
	sidebarBranch  lipgloss.Style
	sidebarRule    lipgloss.Style
	// selectionBG is applied on top of whatever style a row already has, so a
	// selected row keeps its status colour and gains a band.
	selectionBG lipgloss.Color
	badge       lipgloss.Style
	spinner     lipgloss.Style

	popoverBG      lipgloss.Style
	popoverBorder  lipgloss.Style
	popoverTitle   lipgloss.Style
	popoverHeading lipgloss.Style
	popoverKey     lipgloss.Style
	popoverMuted   lipgloss.Style

	tabActive    lipgloss.Style
	tabInactive  lipgloss.Style
	divider      lipgloss.Style
	sidebarEdge  lipgloss.Style
	help         lipgloss.Style
	errorStyle   lipgloss.Style
	alert        lipgloss.Style
	managerReply lipgloss.Style
	managerInput lipgloss.Style
	managerSpin  lipgloss.Style

	// status maps an agent status to its colour, in sidebar and tab flavours.
	status    map[string]lipgloss.Style
	tabStatus map[string]lipgloss.Style
}

func newPalette(c config.Colors) palette {
	col := func(v string) lipgloss.Color { return lipgloss.Color(v) }
	bg := lipgloss.NewStyle().Background(col(c.SidebarBG))
	pop := lipgloss.NewStyle().Background(col(c.PopoverBG))

	p := palette{
		sidebarBG:      bg,
		sidebarHeading: bg.Foreground(col(c.Muted)),
		sidebarItem:    bg.Foreground(col(c.Text)),
		sidebarMuted:   bg.Foreground(col(c.Muted)),
		sidebarActive:  bg.Foreground(col(c.Accent)).Bold(true),
		// The cursor is the selection band plus a bold accent, not reverse
		// video: reverse fights the band, and on a row that already carries a
		// status colour it inverts that colour into the background.
		sidebarCursor: lipgloss.NewStyle().Background(col(c.SelectionBG)).Foreground(col(c.Accent)).Bold(true),
		sidebarBranch: bg.Foreground(col(c.Idle)),
		sidebarRule:   bg.Foreground(col(c.Border)),
		selectionBG:   col(c.SelectionBG),
		badge:         lipgloss.NewStyle().Foreground(col(c.BadgeFG)).Background(col(c.BadgeBG)).Bold(true),
		spinner:       bg.Foreground(col(c.Spinner)).Bold(true),

		popoverBG:      pop.Foreground(col(c.Text)),
		popoverBorder:  pop.Foreground(col(c.Accent)),
		popoverTitle:   pop.Foreground(col(c.Accent)).Bold(true),
		popoverHeading: pop.Foreground(col(c.Muted)).Italic(true),
		popoverKey:     pop.Foreground(col(c.Working)),
		popoverMuted:   pop.Foreground(col(c.Muted)),

		tabActive:   lipgloss.NewStyle().Foreground(col(c.BadgeFG)).Background(col(c.Accent)).Bold(true),
		tabInactive: lipgloss.NewStyle().Foreground(col(c.Muted)),
		divider:     lipgloss.NewStyle().Foreground(col(c.Border)),
		// The sidebar's own edge is drawn with the panel background on one
		// side and the terminal's on the other, so it reads as the boundary
		// of a panel rather than as a stray character.
		sidebarEdge:  lipgloss.NewStyle().Foreground(col(c.SidebarBG)).Background(lipgloss.Color(c.Border)),
		help:         lipgloss.NewStyle().Foreground(col(c.Muted)),
		errorStyle:   lipgloss.NewStyle().Foreground(col(c.Blocked)).Bold(true),
		alert:        lipgloss.NewStyle().Foreground(col(c.Blocked)).Bold(true),
		managerReply: lipgloss.NewStyle().Foreground(col(c.Accent)).Bold(true),
		managerInput: lipgloss.NewStyle().Foreground(col(c.Text)),
		managerSpin:  lipgloss.NewStyle().Foreground(col(c.Spinner)).Bold(true),
	}
	p.status = map[string]lipgloss.Style{
		"working": bg.Foreground(col(c.Working)),
		"blocked": bg.Foreground(col(c.Blocked)).Bold(true),
		"done":    bg.Foreground(col(c.Done)).Bold(true),
		"idle":    bg.Foreground(col(c.Idle)),
	}
	p.tabStatus = map[string]lipgloss.Style{
		"working": lipgloss.NewStyle().Foreground(col(c.Working)),
		"blocked": lipgloss.NewStyle().Foreground(col(c.Blocked)).Bold(true),
		"done":    lipgloss.NewStyle().Foreground(col(c.Done)).Bold(true),
	}
	return p
}

// band puts the selection background behind a row without touching its
// foreground, so a selected agent keeps the colour of its status.
func (p palette) band(style lipgloss.Style) lipgloss.Style {
	return style.Background(p.selectionBG)
}

// sidebarRow records what a rendered sidebar line points at, so a click can
// be turned back into "focus that workspace / that agent" without the client
// having to re-derive the layout it just drew.
type sidebarRow struct {
	kind   string // "workspace" | "agent"
	target string
}

// statusGlyph is the one-character agent state, from the same theme the
// daemon uses for pane headers. A working agent animates; the frame is
// derived from the wall clock, so the sidebar and the pane headers stay in
// step without the two sides syncing anything.
func (m *model) statusGlyph(status, paneStatus string) string {
	return m.icons.Status(status, paneStatus == "exited", m.spinner, time.Now())
}

// anyWorking reports whether something on screen is animating, which is the
// only time the client needs a repaint clock of its own.
func (m *model) anyWorking() bool {
	if m.managerBusy {
		return true
	}
	for _, a := range m.state.Agents {
		if a.Status == "working" {
			return true
		}
	}
	for _, p := range m.state.Panes {
		if p.AgentStatus == "working" {
			return true
		}
	}
	return false
}

// unreadTotal counts agents that finished without being seen — the number
// the badge shows.
func (m *model) unreadTotal() int {
	n := 0
	for _, a := range m.state.Agents {
		if a.Unread {
			n++
		}
	}
	return n
}

// unreadIn counts unseen results inside one workspace.
func (m *model) unreadIn(workspaceName string) int {
	n := 0
	for _, a := range m.state.Agents {
		if a.Unread && a.Workspace == workspaceName {
			n++
		}
	}
	return n
}

// badge renders a count as a filled pill. Zero renders as nothing at all:
// a badge showing "0" is just noise that never goes away.
func (m *model) badge(n int) string {
	if n <= 0 {
		return ""
	}
	return m.p.badge.Render(" " + strconv.Itoa(n) + " ")
}

// renderTabs is the top strip: numbers, coloured by what the tab's agents
// want. Names live in the sidebar, so repeating them here would just be
// noise across the top of every screen.
func (m *model) renderTabs(width int) string {
	if len(m.state.Tabs) == 0 {
		return padTo(m.p.help.Render(" no tabs — prefix c to open one"), width)
	}
	var b strings.Builder
	m.tabHits = m.tabHits[:0]
	col := 0
	for _, t := range m.state.Tabs {
		text := " " + t.Name + " "
		style := m.p.tabInactive
		if s, ok := m.p.tabStatus[t.Status]; ok {
			style = s
		}
		if t.ID == m.state.ActiveTab {
			style = m.p.tabActive
		}
		b.WriteString(style.Render(text))
		m.tabHits = append(m.tabHits, tabHit{id: t.ID, from: col, to: col + len([]rune(text))})
		col += len([]rune(text))
	}
	if m.state.Zoomed {
		b.WriteString(m.p.help.Render(" " + m.icons.Zoom))
	}
	// The total sits at the right-hand end of the strip, so it is visible
	// even when the sidebar is hidden.
	if n := m.unreadTotal(); n > 0 {
		strip := b.String()
		badge := m.badge(n)
		gap := width - lipgloss.Width(strip) - lipgloss.Width(badge)
		if gap > 0 {
			return strip + strings.Repeat(" ", gap) + badge
		}
	}
	return padTo(b.String(), width)
}

type tabHit struct {
	id       string
	from, to int
}

// renderSidebar returns exactly rows lines, each sidebarWidth wide including
// the divider column, so a frame line can be concatenated straight onto it.
//
// Two panels, the way Herdr splits them: the workspaces you could be in, and
// the agents inside them. Plain terminals are deliberately not in the agents
// panel — it answers "who needs me", and a shell at a prompt is not an
// answer. The "spaces" heading belongs to the top row, which View draws
// alongside the tab strip.
func (m *model) renderSidebar(rows int) []string {
	lines := make([]string, 0, rows)
	m.sidebarRows = m.sidebarRows[:0]

	blank := func() {
		lines = append(lines, m.sidebarLine(m.p.sidebarItem, ""))
		m.sidebarRows = append(m.sidebarRows, sidebarRow{})
	}

	for _, w := range m.state.Workspaces {
		style := m.p.sidebarMuted
		if s, ok := m.p.status[w.Status]; ok {
			style = s
		}
		// The marker is the workspace's aggregate status glyph, not a fixed
		// dot: a workspace with a blocked agent inside should look different
		// from one that is merely idle even with colour off (ascii theme, no
		// TTY colour support). The active workspace is already marked by its
		// bold accent style below, so the glyph itself doesn't need to change
		// for that.
		marker := m.statusGlyph(w.Status, "")
		branch := m.p.sidebarBranch
		if w.ID == m.state.ActiveWorkspace {
			style = m.p.sidebarActive
			// The band covers the branch row too, so a two-line entry reads as
			// one selected block rather than a highlighted line with an
			// unrelated line stuck under it.
			style, branch = m.p.band(style), m.p.band(branch)
		}
		if m.navMode && m.navIndex == len(m.sidebarRows) {
			style = m.p.sidebarCursor
		}
		lines = append(lines, m.sidebarLineWith(style,
			marker+" "+w.Name, m.badge(m.unreadIn(w.Name))))
		m.sidebarRows = append(m.sidebarRows, sidebarRow{kind: "workspace", target: w.ID})
		if w.Branch != "" {
			// The branch is context for the line above it, not a row you can
			// click, so it gets no hit target of its own. It is indented to sit
			// under the name, with no glyph of its own: at this size a second
			// symbol per entry is what makes a sidebar look busy.
			lines = append(lines, m.sidebarLine(branch, "  "+w.Branch))
			m.sidebarRows = append(m.sidebarRows, sidebarRow{})
		}
	}

	// The agents panel is pinned to the lower half, the way Herdr splits its
	// sidebar: workspaces stay put at the top while the agent list grows and
	// shrinks underneath, instead of the two lists shoving each other around
	// every time an agent appears.
	half := max(rows/2, 1)
	if len(lines) > half {
		// Too many workspaces to fit their half: clip, and say how many are
		// hidden rather than silently dropping them.
		hidden := len(lines) - (half - 1)
		lines = lines[:half-1]
		m.sidebarRows = m.sidebarRows[:half-1]
		lines = append(lines, m.sidebarLine(m.p.sidebarMuted, " +"+strconv.Itoa(hidden)+" more"))
		m.sidebarRows = append(m.sidebarRows, sidebarRow{})
	}
	for len(lines) < half {
		blank()
	}

	// A rule where the two panels meet. The half-and-half split alone left the
	// agents heading floating in the same field of blank rows as the
	// workspaces, so the sidebar read as one long list with a gap in it.
	lines = append(lines, m.sidebarRule())
	m.sidebarRows = append(m.sidebarRows, sidebarRow{})

	lines = append(lines, m.sidebarLineWith(m.p.sidebarHeading,
		"agents", m.badge(m.unreadTotal())))
	m.sidebarRows = append(m.sidebarRows, sidebarRow{})

	if len(m.state.Agents) == 0 {
		lines = append(lines, m.sidebarLine(m.p.sidebarMuted, " none yet"))
		m.sidebarRows = append(m.sidebarRows, sidebarRow{})
	}
	lastWorkspace := ""
	for _, a := range m.state.Agents {
		if a.Workspace != lastWorkspace {
			lastWorkspace = a.Workspace
			lines = append(lines, m.sidebarLine(m.p.sidebarMuted, " "+a.Workspace))
			m.sidebarRows = append(m.sidebarRows, sidebarRow{})
		}
		style := m.p.sidebarItem
		if s, ok := m.p.status[a.Status]; ok {
			style = s
		}
		// A band, not an underline: an underline in a sidebar row reads as a
		// stray rule under the text rather than as "this one".
		selected := a.PaneID == m.state.Focus
		if selected {
			style = m.p.band(style)
		}
		if m.navMode && m.navIndex == len(m.sidebarRows) {
			style, selected = m.p.sidebarCursor, true
		}
		// An unread result gets a bullet in the left gutter: the whole point
		// of "done" as a separate state is that it is visible at a glance.
		gutter := " "
		if a.Unread {
			gutter = m.icons.Unread
		}
		// The spinner is drawn in its own colour so the one moving thing on
		// screen is also the one thing that stands out. Its background has to
		// follow the row's, or a selected working agent gets a one-cell notch
		// punched in its band.
		glyphStyle := style
		if a.Status == "working" {
			glyphStyle = m.p.spinner
			if selected {
				glyphStyle = m.p.band(glyphStyle)
			}
		}
		lines = append(lines, m.sidebarAgentLine(style, glyphStyle, gutter,
			m.statusGlyph(a.Status, ""), a.Title))
		m.sidebarRows = append(m.sidebarRows, sidebarRow{kind: "agent", target: a.PaneID})
	}

	for len(lines) < rows {
		lines = append(lines, m.sidebarLine(m.p.sidebarItem, ""))
		m.sidebarRows = append(m.sidebarRows, sidebarRow{})
	}
	return lines[:rows]
}

// renderManagerBar draws the manager's own row, directly above the status line:
// the input when you are typing at it, its last answer when you are not, a
// spinner while it is thinking, and otherwise the key that focuses it.
//
// The answer stays until the manager says something else. It deliberately does
// not share the status row: status messages are overwritten by the next
// keypress, and an answer you asked for should not vanish that way.
func (m *model) renderManagerBar() string {
	prefix := " " + m.icons.Agent + " "
	avail := max(m.width-lipgloss.Width(prefix), 0)
	label := m.p.managerReply.Render(prefix)

	switch {
	case m.managerFocused():
		return label + m.p.managerInput.Render(clampPad("❯ "+m.promptText+"▌", avail))
	case m.managerBusy:
		return label + m.p.managerSpin.Render(m.statusGlyph("working", "")) +
			m.p.help.Render(clampPad(" thinking…", max(avail-1, 0)))
	case m.managerReply != "":
		return label + m.p.managerReply.Render(clampPad(m.managerReply, avail))
	default:
		keys := strings.Join(m.keys.KeysFor(config.ActionManager), " ")
		return label + m.p.help.Render(clampPad("❯ ask the manager — "+m.keys.Prefix+" "+keys+", or click here", avail))
	}
}

// sidebarRule draws the horizontal line between the two panels, in the same
// colour as the sidebar's own edge so the panel reads as a box being divided
// rather than as a row of dashes.
func (m *model) sidebarRule() string {
	inner := max(m.sidebarWidth()-1, 0)
	return m.p.sidebarRule.Render(strings.Repeat("─", inner)) + m.p.sidebarEdge.Render("▏")
}

// sidebarLine renders one full-width sidebar row plus the divider column.
func (m *model) sidebarLine(style lipgloss.Style, text string) string {
	return m.sidebarLineWith(style, text, "")
}

// sidebarAgentLine draws one agent row with the status glyph in its own
// colour. It is built from three separately styled spans rather than one
// string because a styled glyph inside the text would be measured as
// escape bytes, mis-padding the row.
func (m *model) sidebarAgentLine(rowStyle, glyphStyle lipgloss.Style, gutter, glyph, title string) string {
	inner := m.sidebarWidth() - 1
	// " " + gutter + glyph + " " + title, padded to the panel width.
	prefixWidth := 3
	return rowStyle.Render(" "+gutter) +
		glyphStyle.Render(glyph) +
		rowStyle.Render(clampPad(" "+title, max(inner-prefixWidth, 0))) +
		m.p.sidebarEdge.Render("▏")
}

// sidebarLineWith renders a row with an already-styled fragment (a badge)
// pinned to the right-hand edge.
//
// The badge has to stay out of the padded text: padding counts runes, and a
// styled fragment is mostly escape sequences, so measuring it as text would
// both mis-align the row and risk truncating an escape in half.
func (m *model) sidebarLineWith(style lipgloss.Style, text, right string) string {
	inner := m.sidebarWidth() - 1
	avail := max(inner-lipgloss.Width(right), 0)
	return style.Render(clampPad(" "+text, avail)) + right + m.p.sidebarEdge.Render("▏")
}

// clampPad forces plain (unstyled) text to exactly w columns.
func clampPad(s string, w int) string {
	r := []rune(s)
	if w < 0 {
		w = 0
	}
	if len(r) > w {
		return string(r[:w])
	}
	return s + strings.Repeat(" ", w-len(r))
}

// padTo appends spaces to an already-styled string so the row reaches width.
// Styled text can't be measured with len, so width comes from lipgloss.
func padTo(s string, width int) string {
	if w := lipgloss.Width(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

// paneLabel prefers the daemon-resolved title, which tracks what is actually
// running in the pane, and falls back to the spawn command.
func paneLabel(p attachproto.PaneSummary) string {
	switch {
	case p.Title != "":
		return p.Title
	case p.Label != "":
		return p.Label
	default:
		return baseName(p.Cmd)
	}
}

func baseName(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// overlay draws a bordered box centred on top of already-rendered lines,
// leaving the multiplexer visible around it. Drawing a popover rather than
// replacing the screen keeps the panes, sidebar and tab strip in view, which
// is the difference between "a dialog in an app" and "the app vanished".
//
// The lines being covered are styled ANSI, so they can't be sliced by byte
// offset. Instead each row the box occupies is redrawn from scratch: the
// sidebar and tab chrome are cheap to regenerate, and the pane content behind
// the box would have to be re-emitted anyway.
func (m *model) overlay(base []string, box []string, width, height int) []string {
	boxW := 0
	for _, line := range box {
		boxW = max(boxW, lipgloss.Width(line))
	}
	boxW = min(boxW+2, width)
	boxH := min(len(box)+2, height)

	left := max((width-boxW)/2, 0)
	top := max((height-boxH)/2, 0)

	out := make([]string, len(base))
	copy(out, base)
	for i, line := range m.boxFrame(box, boxW, boxH) {
		row := top + i
		if row < 0 || row >= len(out) {
			continue
		}
		// Keep whatever was to the left of the box — usually the sidebar —
		// then draw the box over the rest. Anything it covers on the right
		// simply isn't redrawn, which is what "on top" means.
		prefix := m.truncateStyled(out[row], left)
		if w := lipgloss.Width(prefix); w < left {
			prefix += strings.Repeat(" ", left-w)
		}
		out[row] = prefix + line
	}
	return out
}

// boxFrame wraps content in a rounded border sized to boxW x boxH.
func (m *model) boxFrame(content []string, boxW, boxH int) []string {
	inner := max(boxW-2, 0)
	lines := make([]string, 0, boxH)
	lines = append(lines, m.p.popoverBorder.Render("╭"+strings.Repeat("─", inner)+"╮"))
	for i := 0; i < boxH-2; i++ {
		text := ""
		if i < len(content) {
			text = content[i]
		}
		lines = append(lines, m.p.popoverBorder.Render("│")+m.padStyledBG(text, inner)+m.p.popoverBorder.Render("│"))
	}
	lines = append(lines, m.p.popoverBorder.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return lines
}

// padStyledBG pads an already-styled string out to w columns with cells that
// carry the popover's background.
//
// The padding has to be styled separately rather than by wrapping the whole
// row: every lipgloss span ends in a full reset, so a background applied
// around already-styled text stops at the first inner reset and everything
// after it — the padding, and any blank row — renders in the terminal's own
// colour. That is what produced the dark bands across the popover.
func (m *model) padStyledBG(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + m.p.popoverBG.Render(strings.Repeat(" ", d))
	}
	if lipgloss.Width(s) > w {
		return m.truncateStyled(s, w)
	}
	return s
}

// helpEntry is one row of the help popover. Keys come from the live keymap,
// so a rebound key shows its new binding here without anything else changing.
type helpEntry struct{ action, label string }

var helpLeft = []helpEntry{
	{"", "panes"},
	{config.ActionSplitRight, "split right"},
	{config.ActionSplitDown, "split down"},
	{config.ActionClosePane, "close pane"},
	{config.ActionZoom, "zoom pane"},
	{config.ActionFocusLeft, "focus left"},
	{config.ActionFocusDown, "focus down"},
	{config.ActionFocusUp, "focus up"},
	{config.ActionFocusRight, "focus right"},
	{config.ActionResizeLeft, "resize left"},
	{config.ActionResizeRight, "resize right"},
	{config.ActionResizeUp, "resize up"},
	{config.ActionResizeDown, "resize down"},
}

var helpRight = []helpEntry{
	{"", "tabs & workspaces"},
	{config.ActionNewTab, "new tab"},
	{config.ActionNewTabNamed, "new tab, named"},
	{config.ActionCloseTab, "close tab"},
	{config.ActionNextTab, "next tab"},
	{config.ActionPrevTab, "previous tab"},
	{config.ActionRenameTab, "rename tab"},
	{config.ActionNewWorkspace, "new workspace"},
	{config.ActionNextWorkspace, "next workspace"},
	{config.ActionPrevWorkspace, "prev workspace"},
	{config.ActionCloseWorkspce, "close workspace"},
	{"", "view"},
	{config.ActionGoto, "go to (fuzzy)"},
	{config.ActionCopyMode, "scroll / copy mode"},
	{config.ActionGit, "git UI in a pane"},
	{config.ActionManager, "type at the manager"},
	{config.ActionToggleSidebar, "toggle sidebar"},
	{config.ActionFocusSidebar, "sidebar navigation"},
}

// renderHelp builds the popover's contents: two columns of bindings plus the
// paths of the files that define them.
func (m *model) renderHelp(maxWidth int) []string {
	const keyCol, labelCol = 14, 19

	row := func(e helpEntry) string {
		if e.action == "" {
			return m.p.popoverHeading.Render(clampPad(e.label, keyCol+labelCol+1))
		}
		keys := strings.Join(m.keys.KeysFor(e.action), " ")
		return m.p.popoverKey.Render(clampPad(keys, keyCol)) + m.p.popoverBG.Render(clampPad(" "+e.label, labelCol+1))
	}

	out := []string{
		m.p.popoverTitle.Render(" rookery — press " + m.keys.Prefix + ", then:"),
		"",
	}
	for i := 0; i < max(len(helpLeft), len(helpRight)); i++ {
		var l, r string
		if i < len(helpLeft) {
			l = row(helpLeft[i])
		} else {
			l = m.p.popoverBG.Render(strings.Repeat(" ", keyCol+labelCol+1))
		}
		if i < len(helpRight) {
			r = row(helpRight[i])
		}
		out = append(out, l+m.p.popoverBG.Render("  ")+r)
	}

	out = append(out,
		"",
		m.p.popoverKey.Render(clampPad("1-9", keyCol))+m.p.popoverBG.Render(" jump to tab")+
			m.p.popoverBG.Render("      ")+m.p.popoverKey.Render(clampPad(strings.Join(m.keys.KeysFor(config.ActionToggleMouse), " "), 8))+
			m.p.popoverBG.Render(" mouse capture: "+onOff(m.mouseOn)),
		m.p.popoverKey.Render(clampPad(strings.Join(m.keys.KeysFor(config.ActionDetach), " "), keyCol))+
			m.p.popoverBG.Render(" detach, agents keep running"),
		"",
		m.p.popoverMuted.Render(" copy mode: j/k g/G move · v select · y copy · esc exit"),
		m.p.popoverMuted.Render(" mouse: click a pane, tab, workspace or agent · drag a divider · wheel scrolls back"),
		m.p.popoverMuted.Render(" shift+drag for your terminal's own text selection"),
		"",
		m.p.popoverMuted.Render(" "+m.configPath),
		m.p.popoverMuted.Render(" "+m.hotkeysPath),
		"",
		m.p.popoverMuted.Render(" any key closes this"),
	)

	// Nothing here should be wider than the screen it floats over.
	limit := max(maxWidth-4, 10)
	for i, line := range out {
		if lipgloss.Width(line) > limit {
			out[i] = m.truncateStyled(line, limit)
		}
	}
	return out
}

// truncateStyled cuts a styled line to w visible columns. Escape sequences
// are copied through without counting toward the width, so styling survives
// the cut instead of leaking into the rest of the row.
func (m *model) truncateStyled(s string, w int) string {
	var b strings.Builder
	width := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := i
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e && j > i+1) {
				j++
			}
			if j < len(s) {
				j++
			}
			b.WriteString(s[i:j])
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if width+1 > w {
			break
		}
		b.WriteRune(r)
		width++
		i += size
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// tabIndexKey maps "1".."9" to a tab id.
func (m *model) tabIndexKey(key string) string {
	n, err := strconv.Atoi(key)
	if err != nil || n < 1 || n > len(m.state.Tabs) {
		return ""
	}
	return m.state.Tabs[n-1].ID
}
