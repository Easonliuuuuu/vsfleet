package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/easonliuuuuu/vsfleet/internal/assessment"
)

// runsDetailMinWidth is the narrowest terminal that shows the selected run's
// detail beside the list. Below it the detail goes under the list, which
// keeps the list's columns whole at 80 columns.
const runsDetailMinWidth = 100

// runRowWidth is the width of one Runs pane row: cursor, pin, run, label,
// status and start time with their gaps.
const runRowWidth = 57

// runDetailLabelWidth is the field-name gutter of the run detail.
const runDetailLabelWidth = 10

func (m *Model) viewHistoryHubRuns() []string {
	t := m.theme
	// The key hints live in the footer with every other pane's bindings; this
	// line only says how the list is ordered.
	lines := []string{t.title.Render("Runs"), "", t.dim.Render("  newest first")}
	if len(m.runs) == 0 {
		lines = append(lines, t.dim.Render("  no assessments stored"))
		return scrollLines(lines, 0, m.bodyHeight())
	}
	// Cursor, pin and their gaps, then the same widths as the rows.
	lines = append(lines, t.header.Render(truncate(fmt.Sprintf("     %-5s %-18s %-9s %s", "RUN", "LABEL", "STATUS", "STARTED"), m.width)))
	avail := m.bodyHeight() - len(lines)
	detail := m.runDetail(m.runs[clamp(m.runCursor, 0, len(m.runs)-1)])
	if m.width >= runsDetailMinWidth {
		list := m.runRows(avail)
		return append(lines, joinSideBySide(t, list, detail, runRowWidth, m.width-runRowWidth-3, avail)...)
	}
	// Under the list, the detail takes what it needs but leaves the list
	// at least a few rows to move through.
	listH := max(min(len(m.runs), 5), avail-len(detail)-1)
	lines = append(lines, m.runRows(listH)...)
	lines = append(lines, "")
	lines = append(lines, detail...)
	return scrollLines(lines, 0, m.bodyHeight())
}

// runRows is the run list windowed to h rows with the cursor on screen.
func (m *Model) runRows(h int) []string {
	t := m.theme
	if h <= 0 {
		return nil
	}
	start := clamp(m.runCursor-h+1, 0, max(0, len(m.runs)-h))
	var out []string
	for i := start; i < len(m.runs) && i < start+h; i++ {
		r := m.runs[i]
		label := r.Label
		if label == "" {
			label = "—"
		}
		line := fmt.Sprintf("%s%s %-5s %s %-9s %s", listCursor(i == m.runCursor)+" ", runPin(r), historyRunLabel(r.ID), pad(label, 18, false), r.Status, r.StartedAt.Local().Format("2006-01-02 15:04"))
		if i == m.runCursor {
			line = t.focused.Render(line)
		} else {
			line = t.text.Render(line)
		}
		out = append(out, line)
	}
	return out
}

// runDetail is everything stored about one run that its row has no room
// for: which vCenters it reached, which is what makes a run partial, and
// the note, VM count and pin.
func (m *Model) runDetail(r assessment.Run) []string {
	t := m.theme
	field := func(name, value string) string {
		return "  " + t.dim.Render(pad(name, runDetailLabelWidth, false)) + value
	}
	title := historyRunLabel(r.ID)
	if r.Label != "" {
		title += " " + r.Label
	}
	lines := []string{"  " + t.title.Render(title)}
	status := string(r.Status)
	if r.RequestedContexts > 0 {
		status += fmt.Sprintf(" · %d of %d vCenters reached", r.SuccessfulContexts, r.RequestedContexts)
	}
	lines = append(lines, field("STATUS", status))
	coverage := m.historyCoverage[r.ID]
	names := make([]string, 0, len(coverage))
	width := 0
	for name := range coverage {
		names = append(names, name)
		width = max(width, len(name))
	}
	sort.Strings(names)
	for i, name := range names {
		label := ""
		if i == 0 {
			label = "VCENTERS"
		}
		state := t.ok.Render("reached")
		if s := coverage[name]; s != "success" {
			state = t.bad.Render("not reached · " + s)
		}
		lines = append(lines, field(label, pad(name, width+2, false)+state))
	}
	started := r.StartedAt.Local().Format("2006-01-02 15:04")
	if !r.FinishedAt.IsZero() {
		started += " · took " + formatInterval(r.FinishedAt.Sub(r.StartedAt))
	}
	lines = append(lines, field("STARTED", started))
	if r.Source != "" {
		lines = append(lines, field("SOURCE", r.Source))
	}
	lines = append(lines, field("VMs", m.runVMCount(r)))
	pinned := "no"
	if r.Pinned {
		pinned = "yes · cleanup keeps it"
	}
	lines = append(lines, field("PINNED", pinned))
	note := t.faint.Render("none")
	if r.Note != "" {
		note = r.Note
	}
	lines = append(lines, field("NOTE", note))
	return lines
}

// runVMCount is the run's VM count where Trends counted it, in the scope
// Trends counts.
func (m *Model) runVMCount(r assessment.Run) string {
	if m.historyChurn != nil {
		for _, p := range m.historyChurn.Points {
			if p.Run.ID == r.ID {
				return fmt.Sprintf("%d in %s", p.VMCount, m.historyScopeLabel())
			}
		}
	}
	return m.theme.faint.Render("not counted · not in Trends")
}

// handleRunsPaneKey compares from the Runs pane: Enter opens what changed in
// the selected run since the one before it, and b or t make it that end of
// the comparison; Home and End jump the list. It reports whether the key
// was one of those.
func (m *Model) handleRunsPaneKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if len(m.runs) == 0 {
		return nil, false
	}
	switch {
	case key.Matches(msg, m.keys.Home):
		m.runCursor = 0
		return nil, true
	case key.Matches(msg, m.keys.End):
		m.runCursor = len(m.runs) - 1
		return nil, true
	}
	i := clamp(m.runCursor, 0, len(m.runs)-1)
	selected := m.runs[i].ID
	switch {
	case key.Matches(msg, m.keys.Open):
		if i == len(m.runs)-1 {
			m.setMessage(historyRunLabel(selected)+" is the oldest run — there is nothing before it to compare", false)
			return nil, true
		}
		m.baseRun, m.targetRun = m.runs[i+1].ID, selected
	case key.Matches(msg, m.keys.Base):
		if !m.setComparisonEnd("base", selected) {
			return nil, true
		}
	case key.Matches(msg, m.keys.Target):
		if !m.setComparisonEnd("target", selected) {
			return nil, true
		}
	default:
		return nil, false
	}
	m.historyPane = historyPaneChanges
	m.changeCursor, m.changeOffset = 0, 0
	m.centreScrubWindow()
	m.historyErr = nil
	return m.historyDiffCommand(), true
}

// setComparisonEnd makes the run one end of the comparison, for the run
// picker and the Runs pane. A run is never compared with itself, and a pair
// the wrong way round is swapped: a comparison always runs from the older
// run to the newer.
func (m *Model) setComparisonEnd(role string, id int64) bool {
	if (role == "base" && id == m.targetRun) || (role == "target" && id == m.baseRun) {
		m.setMessage("baseline and target must be different runs", true)
		return false
	}
	if role == "base" {
		m.baseRun = id
	} else {
		m.targetRun = id
	}
	if m.runIndex(m.baseRun) < m.runIndex(m.targetRun) {
		m.baseRun, m.targetRun = m.targetRun, m.baseRun
		m.setMessage(historyRunLabel(m.targetRun)+" is newer than "+historyRunLabel(m.baseRun)+" — it is now the target", false)
	}
	return true
}

// runPickerHeading is the column heading of the run picker. Its columns
// follow the picker's rows, which put the B/T role beside the cursor.
func runPickerHeading(width int) string {
	return truncate(strings.TrimRight(fmt.Sprintf("   %-5s %-2s %-18s %-8s %-16s %s", "RUN", "", "LABEL", "STATUS", "STARTED", "SOURCE"), " "), width)
}
