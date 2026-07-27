package tui

import (
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jirkab/rookery/internal/attachproto"
)

// The goto picker: one fuzzy list over everything you can jump to. The
// sidebar is fine at four agents and useless at twenty, which `rook fan`
// produces in a single command — this is the answer to "which pane was the
// one working on the parser".
//
// ponytail: subsequence matching, no ranking library. Over a list this size
// (tens of items, never thousands) the difference between this and a real
// fuzzy scorer is not visible.

// pickerRows is how many matches the popover shows at once.
const pickerRows = 10

type pickerItem struct {
	kind   string // "workspace" | "tab" | "agent"
	id     string // what to focus
	label  string
	detail string
	status string
}

// openPicker builds the list from the state the client already has and shows
// the popover.
func (m *model) openPicker() {
	m.pickerOpen, m.pickerQuery, m.pickerIndex = true, "", 0
	m.pickerItems = m.pickerItems[:0]

	for _, a := range m.state.Agents {
		m.pickerItems = append(m.pickerItems, pickerItem{
			kind: "agent", id: a.PaneID, label: a.Title,
			detail: a.Workspace + " · " + a.Agent, status: a.Status,
		})
	}
	for _, p := range m.state.Panes {
		if p.Agent != "" {
			continue // already listed as an agent, in every workspace not just this tab
		}
		m.pickerItems = append(m.pickerItems, pickerItem{
			kind: "agent", id: p.PaneID, label: paneLabel(p),
			detail: "pane " + p.PaneID, status: p.AgentStatus,
		})
	}
	for _, w := range m.state.Workspaces {
		m.pickerItems = append(m.pickerItems, pickerItem{
			kind: "workspace", id: w.ID, label: w.Name,
			detail: w.Branch, status: w.Status,
		})
	}
	for _, t := range m.state.Tabs {
		m.pickerItems = append(m.pickerItems, pickerItem{
			kind: "tab", id: t.ID, label: "tab " + t.Name,
			detail: m.state.ActiveWorkspace, status: t.Status,
		})
	}
}

func (m *model) closePicker() {
	m.pickerOpen, m.pickerQuery, m.pickerIndex = false, "", 0
}

func (m *model) handlePickerKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	matches := m.pickerMatches()
	switch key {
	case "esc", "ctrl+c":
		m.closePicker()
	case "enter":
		if m.pickerIndex < len(matches) {
			m.activatePickerItem(matches[m.pickerIndex])
		}
		m.closePicker()
	case "down", "ctrl+n":
		if m.pickerIndex+1 < len(matches) {
			m.pickerIndex++
		}
	case "up", "ctrl+p":
		if m.pickerIndex > 0 {
			m.pickerIndex--
		}
	case "backspace":
		if r := []rune(m.pickerQuery); len(r) > 0 {
			m.pickerQuery, m.pickerIndex = string(r[:len(r)-1]), 0
		}
	default:
		if msg.Type == tea.KeyRunes {
			m.pickerQuery, m.pickerIndex = m.pickerQuery+string(msg.Runes), 0
		} else if key == " " {
			m.pickerQuery, m.pickerIndex = m.pickerQuery+" ", 0
		}
	}
	return m, nil
}

func (m *model) activatePickerItem(it pickerItem) {
	switch it.kind {
	case "workspace":
		m.act(attachproto.ActionFocusWS, it.id, "")
	case "tab":
		m.act(attachproto.ActionFocusTab, it.id, "")
	default:
		m.send(attachproto.Focus{Type: attachproto.TypeFocus, PaneID: it.id})
	}
}

// pickerMatches filters and ranks the list against the query. An empty query
// keeps everything, in the order it was built: agents first, because that is
// what you are usually looking for.
func (m *model) pickerMatches() []pickerItem {
	if m.pickerQuery == "" {
		return m.pickerItems
	}
	type scored struct {
		item  pickerItem
		score int
		order int
	}
	var out []scored
	for i, it := range m.pickerItems {
		if s, ok := fuzzyScore(it.label+" "+it.detail, m.pickerQuery); ok {
			out = append(out, scored{item: it, score: s, order: i})
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].score != out[b].score {
			return out[a].score < out[b].score
		}
		return out[a].order < out[b].order
	})
	items := make([]pickerItem, 0, len(out))
	for _, s := range out {
		items = append(items, s.item)
	}
	return items
}

// fuzzyScore matches query as a subsequence of text, case-insensitively, and
// scores by how tightly packed and how early the match is — lower is better.
// Reports false when the query does not match at all.
func fuzzyScore(text, query string) (int, bool) {
	t, q := []rune(strings.ToLower(text)), []rune(strings.ToLower(strings.TrimSpace(query)))
	if len(q) == 0 {
		return 0, true
	}
	score, qi, last := 0, 0, -1
	for ti := 0; ti < len(t) && qi < len(q); ti++ {
		if t[ti] != q[qi] {
			continue
		}
		if last >= 0 {
			score += ti - last - 1 // gaps cost
		} else {
			score += ti // a match that starts late costs
		}
		last, qi = ti, qi+1
	}
	if qi < len(q) {
		return 0, false
	}
	return score, true
}

// renderPicker draws the popover: the query line, then the matches with the
// selected one banded.
func (m *model) renderPicker(maxWidth int) []string {
	width := min(max(maxWidth-8, 24), 60)
	matches := m.pickerMatches()

	out := []string{
		m.p.popoverTitle.Render(clampPad(" go to — type to filter, enter to jump", width)),
		m.p.popoverMuted.Render(clampPad(" ❯ "+m.pickerQuery+"▌", width)),
	}
	if len(matches) == 0 {
		out = append(out, m.p.popoverMuted.Render(clampPad(" nothing matches", width)))
		return out
	}

	// Scroll the window so the cursor stays visible in a long list.
	top := max(min(m.pickerIndex-pickerRows/2, len(matches)-pickerRows), 0)
	for i := top; i < len(matches) && i < top+pickerRows; i++ {
		it := matches[i]
		glyph := m.statusGlyph(it.status, "")
		if it.status == "" {
			glyph = " "
		}
		// A marker as well as a colour: the popover is drawn over pane content
		// in whatever palette the user chose, and one of those two always
		// survives it.
		cursor := "   "
		style := m.p.popoverBG
		if i == m.pickerIndex {
			cursor, style = " ❯ ", m.p.popoverTitle
		}
		text := cursor + glyph + " " + it.label
		if it.detail != "" {
			text += "  " + it.detail
		}
		out = append(out, style.Render(clampPad(text, width)))
	}
	if hidden := len(matches) - min(len(matches), top+pickerRows); hidden > 0 {
		out = append(out, m.p.popoverMuted.Render(clampPad(" +"+strconv.Itoa(hidden)+" more", width)))
	}
	return out
}
