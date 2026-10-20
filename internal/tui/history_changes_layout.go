package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// minChangeRows is the least the change stream keeps on a short terminal,
// its heading included. The header above it gives up its least useful bands
// first so an 80×24 screen still lists the changes it is there to show.
const minChangeRows = 8

// changesHeaderBand is how much of the Changes header is drawn. Each level
// drops one more band, in the order an operator misses them least.
type changesHeaderBand int

const (
	headerFull         changesHeaderBand = iota
	headerNoMoveHint                     // the footer already names ←/→ and b/t
	headerCoverageLine                   // the matrix becomes its one-line verdict
	headerNoRunLines                     // the axis already names both ends
)

// changesHeaderLines is everything above the change stream: the run axis, the
// coverage under it, the identity of the two runs being compared, and the
// counts. It is measured rather than estimated, so the stream's height budget
// is set against exactly what was drawn, and it sheds bands until the stream
// keeps minChangeRows.
func (m *Model) changesHeaderLines() []string {
	var lines []string
	for band := headerFull; band <= headerNoRunLines; band++ {
		lines = m.changesHeader(band)
		if m.bodyHeight()-len(lines) >= minChangeRows {
			break
		}
	}
	return lines
}

func (m *Model) changesHeader(band changesHeaderBand) []string {
	t := m.theme
	var lines []string
	if m.capturing {
		lines = append(lines, "  "+m.spin.View()+t.dim.Render("capturing VM inventory from "+captureScopeLabel(m.capturingStates())+"…"))
	}
	// The capture-size row is the first thing to go on a short terminal: it
	// is the only band on this screen that is decoration rather than an
	// answer, since the counts below say what actually changed.
	lines = append(lines, m.viewScrubberWith(m.width, m.bodyHeight() >= 18, band < headerNoMoveHint)...)
	if band < headerCoverageLine {
		lines = append(lines, m.viewCoverage(m.width)...)
	} else if line := m.coverageLine(); line != "" {
		lines = append(lines, line)
	}
	if m.changeDiff == nil {
		return append(lines, "")
	}
	bar := m.viewComparisonBar(m.changeDiff)
	if band >= headerNoRunLines {
		// The baseline and target lines lead the bar; the warnings after
		// them are what the counts cannot be read without.
		bar = bar[min(2, len(bar)):]
	}
	lines = append(lines, bar...)
	c := m.changeDiff.Counts
	counts := strings.Join([]string{
		countSegment(t, c.Vanished, "vanished", t.bad),
		countSegment(t, c.Appeared, "appeared", t.ok),
		countSegment(t, c.Moved, "moved", t.warn),
		countSegment(t, c.Modified, "modified", t.warn),
		countSegment(t, c.Resources, "infra", t.warn),
		countSegment(t, c.Snapshots, "snapshots", t.warn),
	}, t.faint.Render("   "))
	// The stream lists the vCenters in scope, while the axis and coverage
	// describe every vCenter a run reached; the scope says which the counts
	// are, the way Trends labels its own.
	lines = append(lines, truncate("  "+t.dim.Render(m.historyScopeLabel()+" · ")+counts, m.width))
	if m.impactFilter != impactAll {
		lines = append(lines, truncate("  "+t.accent.Render(m.impactFilter.String())+t.dim.Render(" only — "+m.impactFilter.why()+" · 0 clears"), m.width))
	}
	return append(lines, "")
}

// coverageLine is the coverage matrix as one line, for a terminal too short
// for the matrix: the gap and its fix when the two ends differ, otherwise
// whether every visible run reached the same vCenters.
func (m *Model) coverageLine() string {
	t := m.theme
	if hint := m.coverageHint(); hint != "" {
		return truncate("  "+t.warn.Render(hint), m.width)
	}
	lines := m.viewCoverage(m.width)
	if len(lines) <= 1 {
		return strings.Join(lines, "")
	}
	start, count := scrubWindow(m.runs, m.scrubOffset, m.width)
	visible := m.runs[start : start+count]
	var gaps []string
	for _, name := range m.coverageNames(visible) {
		for _, r := range visible {
			if m.historyCoverage[r.ID][name] != "success" {
				gaps = append(gaps, name)
				break
			}
		}
	}
	return truncate("  "+pad("COVER", scrubLabelWidth, false)+t.warn.Render("not reached in every run: "+strings.Join(gaps, ", ")), m.width)
}

// windowInspector fits an inspector into h rows at the operator's scroll
// position. Clipped lines are counted rather than silently cut, so a group
// of 39 VMs never reads as the 33 that fit.
func (m *Model) windowInspector(lines []string, h int) []string {
	if h <= 0 {
		return nil
	}
	if m.inspectorCursor != m.changeCursor {
		m.inspectorCursor, m.inspectorOffset = m.changeCursor, 0
	}
	if len(lines) <= h {
		m.inspectorOffset = 0
		return lines
	}
	// One row each way may be given up to say what is above and below.
	m.inspectorOffset = clamp(m.inspectorOffset, 0, len(lines)-h+1)
	out := scrollLines(lines, m.inspectorOffset, h)
	if m.inspectorOffset > 0 {
		out[0] = m.theme.dim.Render(fmt.Sprintf("  … %d above · pgup", m.inspectorOffset+1))
	}
	if below := len(lines) - (m.inspectorOffset + h); below > 0 {
		out[len(out)-1] = m.theme.dim.Render(fmt.Sprintf("  … +%d more · pgdn", below+1))
	}
	return out
}

// scrollInspector moves the inspector a page for PgUp/PgDn and reports
// whether the key was one of them. Up and Down stay with the change list.
func (m *Model) scrollInspector(msg tea.KeyMsg) bool {
	page := max(1, m.bodyHeight()/2)
	switch {
	case key.Matches(msg, m.keys.PageUp):
		m.inspectorOffset = max(0, m.inspectorOffset-page)
	case key.Matches(msg, m.keys.PageDown):
		m.inspectorOffset += page
	default:
		return false
	}
	return true
}

// changesHelpSections is the History help for this terminal. Beside the split
// inspector Enter opens nothing, so help does not offer it there.
func (m *Model) changesHelpSections() []helpSection {
	sections := m.keys.historyHelpSections(m.historyPane, m.canCapture())
	if m.historyPane != historyPaneChanges || m.historySplitWidth() == 0 {
		return sections
	}
	for i, sec := range sections {
		kept := make([]key.Binding, 0, len(sec.bindings))
		for _, b := range sec.bindings {
			if b.Help().Key != m.keys.Open.Help().Key {
				kept = append(kept, b)
			}
		}
		sections[i].bindings = kept
	}
	return sections
}
