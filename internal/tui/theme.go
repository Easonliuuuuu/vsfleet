package tui

import "github.com/charmbracelet/lipgloss"

// Glyphs. These match the ones the command line already uses, so that a
// connected vCenter looks the same whichever way you asked.
const (
	glyphOnline  = "●"
	glyphPending = "◐"
	glyphOffline = "○"
	glyphFail    = "✕"
	glyphOK      = "✓"
	glyphSkip    = "–"
	glyphCursor  = "▸"
)

// Colours are adaptive: a terminal with a light background gets darker inks.
// Nothing here is load-bearing — every state that has a colour also has a
// glyph and a word, because operators run this over SSH in whatever palette
// the jump host was configured with.
var (
	colText    = lipgloss.AdaptiveColor{Light: "#1c1c1c", Dark: "#e4e4e4"}
	colDim     = lipgloss.AdaptiveColor{Light: "#6c6c6c", Dark: "#8a8a8a"}
	colFaint   = lipgloss.AdaptiveColor{Light: "#9e9e9e", Dark: "#5f5f5f"}
	colAccent  = lipgloss.AdaptiveColor{Light: "#0057b7", Dark: "#7aa2f7"}
	colOK      = lipgloss.AdaptiveColor{Light: "#116329", Dark: "#7ee787"}
	colWarn    = lipgloss.AdaptiveColor{Light: "#8a5300", Dark: "#e3b341"}
	colBad     = lipgloss.AdaptiveColor{Light: "#a40e26", Dark: "#ff7b72"}
	colRule    = lipgloss.AdaptiveColor{Light: "#d0d0d0", Dark: "#3a3a3a"}
	colSelBg   = lipgloss.AdaptiveColor{Light: "#dbe9ff", Dark: "#2a3348"}
	colFocusBg = lipgloss.AdaptiveColor{Light: "#c3d9ff", Dark: "#3b4a6b"}
)

type theme struct {
	title    lipgloss.Style
	subtitle lipgloss.Style
	text     lipgloss.Style
	dim      lipgloss.Style
	faint    lipgloss.Style
	accent   lipgloss.Style
	ok       lipgloss.Style
	warn     lipgloss.Style
	bad      lipgloss.Style
	rule     lipgloss.Style
	header   lipgloss.Style
	tabOn    lipgloss.Style
	tabOff   lipgloss.Style
	// selected marks the cursor row in a pane that does not have focus,
	// focused the one in the pane that does. Two shades rather than one is
	// what makes "which pane do my arrow keys go to" answerable at a glance.
	selected lipgloss.Style
	focused  lipgloss.Style
	label    lipgloss.Style
	value    lipgloss.Style
}

func newTheme() theme {
	return theme{
		title:    lipgloss.NewStyle().Bold(true).Foreground(colAccent),
		subtitle: lipgloss.NewStyle().Foreground(colDim),
		text:     lipgloss.NewStyle().Foreground(colText),
		dim:      lipgloss.NewStyle().Foreground(colDim),
		faint:    lipgloss.NewStyle().Foreground(colFaint),
		accent:   lipgloss.NewStyle().Foreground(colAccent),
		ok:       lipgloss.NewStyle().Foreground(colOK),
		warn:     lipgloss.NewStyle().Foreground(colWarn),
		bad:      lipgloss.NewStyle().Foreground(colBad),
		rule:     lipgloss.NewStyle().Foreground(colRule),
		header:   lipgloss.NewStyle().Bold(true).Foreground(colDim),
		tabOn:    lipgloss.NewStyle().Bold(true).Foreground(colAccent).Underline(true),
		tabOff:   lipgloss.NewStyle().Foreground(colDim),
		selected: lipgloss.NewStyle().Background(colSelBg).Foreground(colText),
		focused:  lipgloss.NewStyle().Background(colFocusBg).Foreground(colText).Bold(true),
		label:    lipgloss.NewStyle().Foreground(colDim),
		value:    lipgloss.NewStyle().Foreground(colText),
	}
}

// statusStyle maps a row status onto its ink.
func (t theme) statusStyle(s rowStatus) lipgloss.Style {
	switch s {
	case statusGood:
		return t.ok
	case statusWarn:
		return t.warn
	case statusBad:
		return t.bad
	default:
		return t.faint
	}
}
