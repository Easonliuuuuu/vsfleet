package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/easonliuuuuu/vsfleet/internal/whatsnew"
)

// The "What's new" popup is the same 60-column box as the upgrade prompt, so
// a notes title (whatsnew.MaxLine) always fits its 54 columns of text.
const (
	whatsNewBoxWidth = upgradeBoxWidth
	whatsNewInner    = whatsNewBoxWidth - 6
	// whatsNewListMax is the widest the full list grows to.
	whatsNewListMax = 80
)

// whatsNewPanel shows, after an upgrade, what the releases since the last
// one this person ran brought: a short popup of headlines with "w" for the
// full list. It plays after the welcome and before any upgrade prompt, and
// like them hands Bubble Tea the next model when it is done, so the
// interface underneath carries on loading the whole time.
type whatsNewPanel struct {
	inner    *Model
	next     tea.Model
	releases []whatsnew.Release
	list     *whatsNewList
	width    int
	height   int
	sized    bool
}

func newWhatsNewPanel(inner *Model, next tea.Model, releases []whatsnew.Release) *whatsNewPanel {
	return &whatsNewPanel{inner: inner, next: next, releases: releases}
}

func (p *whatsNewPanel) base() *Model { return p.inner }

func (p *whatsNewPanel) Init() tea.Cmd { return p.next.Init() }

func (p *whatsNewPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height, p.sized = msg.Width, msg.Height, true
		cmd := p.forward(msg)
		if p.popup() == "" {
			p.inner.setMessage(whatsNewNote(p.releases), false)
			return p.next, cmd
		}
		return p, cmd
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return p.next.Update(msg)
		}
		if p.list != nil {
			if p.list.key(msg) {
				p.list = nil
			}
			return p, nil
		}
		if msg.String() == "w" {
			p.list = &whatsNewList{releases: p.releases}
			return p, nil
		}
		// Any other key closes the popup and is not passed on: it was
		// pressed to dismiss the notes, not to act on the table.
		return p.next, nil
	case tea.MouseMsg:
		return p, nil
	}
	return p, p.forward(msg)
}

func (p *whatsNewPanel) forward(msg tea.Msg) tea.Cmd {
	next, cmd := p.next.Update(msg)
	p.next = next
	return cmd
}

func (p *whatsNewPanel) View() string {
	if !p.sized {
		return ""
	}
	box := p.popup()
	if p.list != nil {
		box = p.list.render(p.inner.theme, p.width, p.height, "esc back")
	}
	return overlay(p.inner.View(), box, p.width, p.height, p.inner.theme.faint)
}

// popup is the headline box, shortened to the terminal: "try" lines go
// first, then the later highlights, down to one. Heads-up items always
// stay, since they are what someone upgrading must not miss. It is "" when
// even that does not fit.
func (p *whatsNewPanel) popup() string {
	if p.width < whatsNewBoxWidth {
		return ""
	}
	highlights := popupHighlights(p.releases)
	for _, tries := range []bool{true, false} {
		for n := len(highlights); n >= 1; n-- {
			box := p.box(highlights[:n], tries)
			if lipgloss.Height(box) <= p.height {
				return box
			}
		}
	}
	return ""
}

func (p *whatsNewPanel) box(highlights []whatsnew.Item, tries bool) string {
	t := p.inner.theme
	bright := t.text.Bold(true)
	fit := func(s string) string { return ansi.Truncate(s, whatsNewInner, "…") }
	lines := []string{fit(bright.Render(whatsNewHeading(p.releases))), ""}
	for _, it := range highlights {
		lines = append(lines, fit(t.ok.Render("★ ")+t.text.Render(it.Title)))
		if tries && it.Try != "" {
			lines = append(lines, fit("  "+t.dim.Render("try  ")+t.accent.Render(it.Try)))
		}
	}
	if heads := headsUp(p.releases); len(heads) > 0 {
		lines = append(lines, "")
		shown := min(len(heads), whatsnew.MaxHeadsUp)
		for _, it := range heads[:shown] {
			lines = append(lines, fit(t.warn.Render("⚠ ")+t.text.Render(it.Title)))
		}
		if more := len(heads) - shown; more > 0 {
			lines = append(lines, fit(t.dim.Render("  + "+strconv.Itoa(more)+" more in the full list")))
		}
	}
	lines = append(lines, "", t.title.Render("w")+" "+t.dim.Render("full list")+"   "+t.dim.Render("any other key to close"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 2).
		Width(whatsNewBoxWidth - 2).
		Render(strings.Join(lines, "\n"))
}

// whatsNewHeading names one release, or the span of several.
func whatsNewHeading(releases []whatsnew.Release) string {
	if len(releases) == 1 {
		return "What's new in vsfleet " + displayVersion(releases[0].Version)
	}
	return "What's new in vsfleet " + displayVersion(releases[len(releases)-1].Version) + " – " + displayVersion(releases[0].Version)
}

// whatsNewNote is the notes as one line, for a terminal too small for the
// popup.
func whatsNewNote(releases []whatsnew.Release) string {
	note := whatsNewHeading(releases)
	if h := popupHighlights(releases); len(h) > 0 {
		note += ": " + h[0].Title
		if len(h) > 1 {
			note += " and more"
		}
	}
	return note
}

// popupHighlights are the newest releases' highlights, as many as fit.
func popupHighlights(releases []whatsnew.Release) []whatsnew.Item {
	var out []whatsnew.Item
	for _, r := range releases {
		for _, it := range r.Highlights {
			if len(out) == whatsnew.MaxHighlights {
				return out
			}
			out = append(out, it)
		}
	}
	return out
}

// headsUp is every release's heads-up items, oldest release last.
func headsUp(releases []whatsnew.Release) []whatsnew.Item {
	var out []whatsnew.Item
	for _, r := range releases {
		out = append(out, r.HeadsUp...)
	}
	return out
}

// whatsNewList is the full notes, scrolled in a box that fills most of the
// terminal: every release's highlights, heads-up items and fixes with their
// details.
type whatsNewList struct {
	releases []whatsnew.Release
	offset   int
	// rows is how many lines the last render showed, for paging.
	rows, total int
}

// key handles a key and reports whether it closes the list.
func (l *whatsNewList) key(msg tea.KeyMsg) bool {
	page := max(l.rows-1, 1)
	switch msg.String() {
	case "esc", "q", "w", "enter":
		return true
	case "down", "j":
		l.offset++
	case "up", "k":
		l.offset--
	case "pgdown", " ", "f":
		l.offset += page
	case "pgup", "b":
		l.offset -= page
	case "home", "g":
		l.offset = 0
	case "end", "G":
		l.offset = l.total
	}
	l.offset = max(min(l.offset, l.total-l.rows), 0)
	return false
}

func (l *whatsNewList) render(t theme, width, height int, back string) string {
	boxWidth := min(width-2, whatsNewListMax)
	inner := boxWidth - 6
	body := l.lines(t, inner)
	// Border and padding take four rows, the footer two.
	l.rows = max(height-2-6, 1)
	l.total = len(body)
	l.offset = max(min(l.offset, l.total-l.rows), 0)
	end := min(l.offset+l.rows, len(body))
	shown := append([]string(nil), body[l.offset:end]...)
	for len(shown) < min(l.rows, len(body)) {
		shown = append(shown, "")
	}
	footer := t.dim.Render(back)
	if l.total > l.rows {
		footer = t.dim.Render("↑↓ scroll · " + back + " · " + strconv.Itoa(min(end, l.total)) + "/" + strconv.Itoa(l.total))
	}
	shown = append(shown, "", footer)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 2).
		Width(boxWidth - 2).
		Render(strings.Join(shown, "\n"))
}

func (l *whatsNewList) lines(t theme, inner int) []string {
	var out []string
	wrap := func(s string, indent int, style lipgloss.Style) {
		for _, line := range strings.Split(ansi.Wordwrap(s, inner-indent, ""), "\n") {
			out = append(out, strings.Repeat(" ", indent)+style.Render(line))
		}
	}
	section := func(mark lipgloss.Style, glyph string, items []whatsnew.Item) {
		for i, it := range items {
			if i > 0 {
				out = append(out, "")
			}
			out = append(out, ansi.Truncate(mark.Render(glyph+" ")+t.text.Bold(true).Render(it.Title), inner, "…"))
			if it.Detail != "" {
				wrap(it.Detail, 2, t.text)
			}
			if it.Try != "" {
				out = append(out, ansi.Truncate("  "+t.dim.Render("try  ")+t.accent.Render(it.Try), inner, "…"))
			}
		}
	}
	for i, r := range l.releases {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, t.title.Render("vsfleet "+displayVersion(r.Version)), "")
		section(t.ok, "★", r.Highlights)
		if len(r.HeadsUp) > 0 {
			out = append(out, "", t.warn.Render("Heads up"))
			section(t.warn, "⚠", r.HeadsUp)
		}
		if len(r.Fixes) > 0 {
			out = append(out, "", t.dim.Render("Fixes"))
			section(t.dim, "•", r.Fixes)
		}
		out = append(out, "", t.faint.Render(strings.TrimPrefix(releasePage(r.Version), "https://")))
	}
	return out
}

func releasePage(version string) string {
	return "https://" + welcomeRepo + "/releases/tag/" + displayVersion(version)
}
