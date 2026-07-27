package config

// Colors is the UI palette. Values are anything lipgloss accepts: an ANSI
// index ("6"), a 256-colour index ("110"), or a hex string ("#7aa2f7").
//
// Defaults are 256-colour indices rather than hex so the chrome still looks
// deliberate in a terminal that isn't truecolor, and they lean on the low
// ANSI slots for status colours so they follow whatever theme the terminal
// already uses.
type Colors struct {
	Accent      string `json:"accent"`       // focus, active tab, selection
	Working     string `json:"working"`      // an agent mid-turn
	Spinner     string `json:"spinner"`      // the animated frame itself
	Blocked     string `json:"blocked"`      // an agent waiting on you
	Done        string `json:"done"`         // finished, unseen
	Idle        string `json:"idle"`         // finished, seen
	Muted       string `json:"muted"`        // secondary text
	Border      string `json:"border"`       // dividers and popover edges
	SidebarBG   string `json:"sidebar_bg"`   // the sidebar panel
	SelectionBG string `json:"selection_bg"` // the band behind the selected row
	PopoverBG   string `json:"popover_bg"`   // the help popover
	Text        string `json:"text"`         // primary sidebar text
	BadgeFG     string `json:"badge_fg"`     // unread badge foreground
	BadgeBG     string `json:"badge_bg"`     // unread badge background
	HeaderFG    string `json:"header_fg"`    // pane header text
	HeaderFocus string `json:"header_focus"` // focused pane header text
}

func DefaultColors() Colors {
	return Colors{
		Accent:    "110", // soft blue
		Working:   "6",   // cyan
		Spinner:   "208", // orange — the one moving thing on screen
		Blocked:   "3",   // yellow — the one that should catch your eye
		Done:      "2",   // green
		Idle:      "245",
		Muted:     "240",
		Border:    "238",
		SidebarBG: "235",
		// A hair blue rather than a lighter grey: at this contrast a grey band
		// reads as "this row is a different kind of row", and a tinted one
		// reads as "this row is selected", which is the thing being said.
		SelectionBG: "#32324a",
		PopoverBG:   "236",
		Text:        "250",
		BadgeFG:     "232",
		BadgeBG:     "3",
		HeaderFG:    "244",
		HeaderFocus: "110",
	}
}

// merge fills any colour the user left out with its default, so a partial
// "colors" block in config.json is valid rather than a mostly-black UI.
func (c Colors) merge() Colors {
	d := DefaultColors()
	pick := func(v, fallback string) string {
		if v == "" {
			return fallback
		}
		return v
	}
	return Colors{
		Accent:      pick(c.Accent, d.Accent),
		Working:     pick(c.Working, d.Working),
		Spinner:     pick(c.Spinner, d.Spinner),
		Blocked:     pick(c.Blocked, d.Blocked),
		Done:        pick(c.Done, d.Done),
		Idle:        pick(c.Idle, d.Idle),
		Muted:       pick(c.Muted, d.Muted),
		Border:      pick(c.Border, d.Border),
		SidebarBG:   pick(c.SidebarBG, d.SidebarBG),
		SelectionBG: pick(c.SelectionBG, d.SelectionBG),
		PopoverBG:   pick(c.PopoverBG, d.PopoverBG),
		Text:        pick(c.Text, d.Text),
		BadgeFG:     pick(c.BadgeFG, d.BadgeFG),
		BadgeBG:     pick(c.BadgeBG, d.BadgeBG),
		HeaderFG:    pick(c.HeaderFG, d.HeaderFG),
		HeaderFocus: pick(c.HeaderFocus, d.HeaderFocus),
	}
}
