package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
)

func (m *Model) enterChanges() tea.Cmd {
	m.mode = modeChanges
	m.filter.Placeholder = "filter changes"
	m.filtering = false
	m.filter.Blur()
	m.changeCursor, m.changeOffset = 0, 0
	if m.assessment == nil {
		m.historyErr = fmt.Errorf("historical assessments are unavailable")
		return nil
	}
	m.historyErr = nil
	return loadHistoryRunsCmd(m.ctx, m.assessment)
}

func (m *Model) loadDefaultHistoryDiff() {
	if len(m.runs) < 2 {
		m.changeDiff = nil
		m.baseRun, m.targetRun = 0, 0
		m.historyErr = fmt.Errorf("capture at least two assessments to compare")
		return
	}
	// Runs are newest first. The newest pair is the useful default and is
	// deterministic even when an intervening run failed.
	m.baseRun, m.targetRun = m.runs[1].ID, m.runs[0].ID
	m.historyErr = nil
}

func (m *Model) historyDiffCommand() tea.Cmd {
	if m.assessment == nil || len(m.runs) < 2 {
		return nil
	}
	if m.baseRun == 0 || m.targetRun == 0 || m.baseRun == m.targetRun {
		return nil
	}
	return loadHistoryDiffCmd(m.ctx, m.assessment, m.baseRun, m.targetRun)
}

func (m *Model) changeRows() []historyRow {
	if m.changeDiff == nil {
		return nil
	}
	var rows []historyRow
	for _, v := range m.changeDiff.VMs {
		rows = append(rows, historyRow{label: v.Name, context: v.Context, change: strings.Join(v.Changes, ", "), detail: changeDetail(v)})
	}
	for _, s := range m.changeDiff.Snapshots {
		rows = append(rows, historyRow{label: s.VMName + " / " + s.Name, context: s.Context, change: "snapshot " + s.Kind, detail: nonempty(s.After, s.Before)})
	}
	needle := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if needle == "" {
		return rows
	}
	out := rows[:0]
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.label), needle) || strings.Contains(strings.ToLower(r.context), needle) || strings.Contains(strings.ToLower(r.change), needle) {
			out = append(out, r)
		}
	}
	return out
}

type historyRow struct{ label, context, change, detail string }

func (m *Model) viewChangesHeader() string {
	t := m.theme
	label := "history"
	if m.targetRun != 0 {
		label = fmt.Sprintf("changes  ·  target #%d", m.targetRun)
	} else if len(m.runs) > 0 {
		label = fmt.Sprintf("changes  ·  target #%d", m.runs[0].ID)
	}
	return t.title.Render("vsfleet") + "  " + t.accent.Render(label)
}

func (m *Model) handleChangesKey(msg tea.KeyMsg) tea.Cmd {
	rows := m.changeRows()
	switch {
	case key.Matches(msg, m.keys.History):
		m.mode = modeBrowse
		m.filter.Placeholder = filterPlaceholder
	case key.Matches(msg, m.keys.Back):
		m.mode = modeBrowse
		m.filter.Placeholder = filterPlaceholder
	case key.Matches(msg, m.keys.Timeline):
		if len(rows) == 0 || m.changeCursor >= len(rows) || m.assessment == nil {
			return nil
		}
		query := rows[m.changeCursor].label
		if i := strings.Index(query, " / "); i >= 0 {
			query = query[:i]
		}
		m.timelineQuery = query
		m.timelineAll = false
		m.timelineCursor, m.timelineOffset = 0, 0
		m.historyErr = nil
		m.mode = modeHistoryTimeline
		return loadHistoryTimelineCmd(m.ctx, m.assessment, query, false, false)
	case key.Matches(msg, m.keys.Up):
		m.changeCursor = clamp(m.changeCursor-1, 0, max(0, len(rows)-1))
	case key.Matches(msg, m.keys.Down):
		m.changeCursor = clamp(m.changeCursor+1, 0, max(0, len(rows)-1))
	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		return m.filter.Focus()
	case key.Matches(msg, m.keys.Capture):
		if m.assessment == nil || m.capturing {
			return nil
		}
		m.capturing = true
		m.historyErr = nil
		m.setMessage("capturing all configured contexts…", false)
		return captureHistoryCmd(m.ctx, m.assessment, m.backend.Contexts())
	case key.Matches(msg, m.keys.Base), key.Matches(msg, m.keys.Target):
		if len(m.runs) == 0 {
			return nil
		}
		if key.Matches(msg, m.keys.Base) {
			m.pickerRole = "base"
			m.runCursor = m.runIndex(m.baseRun)
		} else {
			m.pickerRole = "target"
			m.runCursor = m.runIndex(m.targetRun)
		}
		m.mode = modeHistoryRuns
		return nil
	case key.Matches(msg, m.keys.Open):
		if len(rows) > 0 && m.changeCursor < len(rows) {
			m.mode = modeChangeDetail
		}
	}
	return nil
}

func (m *Model) runIndex(id int64) int {
	for i, r := range m.runs {
		if r.ID == id {
			return i
		}
	}
	return 0
}

func (m *Model) handleHistoryRunsKey(msg tea.KeyMsg) tea.Cmd {
	if len(m.runs) == 0 {
		if key.Matches(msg, m.keys.Back) {
			m.mode = modeChanges
		}
		return nil
	}
	switch {
	case key.Matches(msg, m.keys.Back):
		m.mode = modeChanges
	case key.Matches(msg, m.keys.Up):
		m.runCursor = clamp(m.runCursor-1, 0, len(m.runs)-1)
	case key.Matches(msg, m.keys.Down):
		m.runCursor = clamp(m.runCursor+1, 0, len(m.runs)-1)
	case key.Matches(msg, m.keys.Home):
		m.runCursor = 0
	case key.Matches(msg, m.keys.End):
		m.runCursor = len(m.runs) - 1
	case key.Matches(msg, m.keys.Open):
		selected := m.runs[clamp(m.runCursor, 0, len(m.runs)-1)].ID
		if (m.pickerRole == "base" && selected == m.targetRun) || (m.pickerRole == "target" && selected == m.baseRun) {
			m.historyErr = fmt.Errorf("baseline and target must be different runs")
			return nil
		}
		if m.pickerRole == "base" {
			m.baseRun = selected
		} else {
			m.targetRun = selected
		}
		m.mode = modeChanges
		m.historyErr = nil
		return m.historyDiffCommand()
	}
	return nil
}

func (m *Model) viewHistoryRuns() []string {
	t := m.theme
	role := m.pickerRole
	if role == "" {
		role = "run"
	}
	lines := []string{t.title.Render("Select " + role + " assessment"), "", t.dim.Render("  newest first · enter selects · esc cancels")}
	for i, r := range m.runs {
		marker := "  "
		if r.ID == m.baseRun {
			marker = "B "
		}
		if r.ID == m.targetRun {
			marker = "T "
		}
		label := r.Label
		if label == "" {
			label = "—"
		}
		if r.Pinned {
			label = "📌 " + label
		}
		line := fmt.Sprintf("%s%-5s %-18s %-8s %-10s %s", marker, historyRunLabel(r.ID), truncate(label, 18), r.Status, r.StartedAt.Local().Format("2006-01-02 15:04"), r.Source)
		if i == m.runCursor {
			line = t.focused.Render(line)
		} else {
			line = t.text.Render(line)
		}
		lines = append(lines, line)
	}
	return scrollLines(lines, 0, m.bodyHeight())
}

func (m *Model) viewChanges() []string {
	t := m.theme
	lines := []string{t.title.Render("Changes")}
	if m.baseRun != 0 && m.targetRun != 0 {
		lines = append(lines, t.dim.Render(fmt.Sprintf("baseline #%d  →  target #%d", m.baseRun, m.targetRun)), "")
	}
	if m.capturing {
		lines = append(lines, "  "+m.spin.View()+t.dim.Render("capturing VM inventory from every configured vCenter…"), "")
	}
	if m.historyErr != nil {
		return append(lines, "  "+t.warn.Render(m.historyErr.Error()), t.dim.Render("  press n to capture an assessment"))
	}
	if m.changeDiff == nil {
		return append(lines, t.dim.Render("no comparable assessments"))
	}
	lines = append(lines, t.header.Render(fmt.Sprintf("  +%d appeared  -%d vanished  →%d moved  ~%d modified  snapshots:%d", m.changeDiff.Counts.Appeared, m.changeDiff.Counts.Vanished, m.changeDiff.Counts.Moved, m.changeDiff.Counts.Modified, m.changeDiff.Counts.Snapshots)), "")
	for _, w := range m.changeDiff.Warnings {
		lines = append(lines, "  "+t.warn.Render("! "+w))
	}
	rows := m.changeRows()
	if len(rows) == 0 {
		lines = append(lines, t.dim.Render("no changes in the selected assessments"))
		return lines
	}
	lines = append(lines, t.header.Render("  CHANGE              VM / SNAPSHOT                         CONTEXT"))
	for i := m.changeOffset; i < len(rows) && i < m.changeOffset+m.bodyHeight()-len(lines); i++ {
		r := rows[i]
		line := fmt.Sprintf("  %-18s %-38s %s", truncate(r.change, 18), truncate(r.label, 38), r.context)
		if i == m.changeCursor {
			line = t.focused.Render(line)
		} else {
			line = t.text.Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}

func (m *Model) viewChangeDetail() []string {
	t := m.theme
	rows := m.changeRows()
	if m.changeCursor < 0 || m.changeCursor >= len(rows) {
		return []string{t.dim.Render("nothing selected")}
	}
	r := rows[m.changeCursor]
	lines := []string{t.title.Render(r.label), "", "  " + t.label.Render("Context") + "  " + r.context, "  " + t.label.Render("Change") + "   " + r.change, ""}
	if r.detail != "" {
		for _, line := range wrap(r.detail, m.width-4) {
			lines = append(lines, "  "+t.value.Render(line))
		}
	}
	if m.changeDiff != nil {
		for _, v := range m.changeDiff.VMs {
			if v.Name == r.label && v.Context == r.context {
				lines = append(lines, "", t.header.Render("Field changes"))
				for _, f := range v.Fields {
					lines = append(lines, fmt.Sprintf("  %-18s %s → %s", f.Field, truncate(nonempty(f.Before, "—"), 24), truncate(nonempty(f.After, "—"), 24)))
				}
				if v.MatchBasis != "" {
					lines = append(lines, "", t.dim.Render("  matched by "+v.MatchBasis))
				}
				break
			}
		}
	}
	return scrollLines(lines, 0, m.bodyHeight())
}

func (m *Model) handleHistoryTimelineKey(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, m.keys.Back) {
		m.mode = modeChangeDetail
		return nil
	}
	if key.Matches(msg, m.keys.Up) {
		m.timelineCursor = clamp(m.timelineCursor-1, 0, max(0, len(m.timeline)-1))
	}
	if key.Matches(msg, m.keys.Down) {
		m.timelineCursor = clamp(m.timelineCursor+1, 0, max(0, len(m.timeline)-1))
	}
	if key.Matches(msg, m.keys.TimelineAll) {
		m.timelineAll = !m.timelineAll
		if m.assessment != nil {
			return loadHistoryTimelineCmd(m.ctx, m.assessment, m.timelineQuery, m.timelineAll, false)
		}
	}
	if key.Matches(msg, m.keys.Open) && len(m.timeline) > 0 && m.timelineCursor < len(m.timeline) {
		m.mode = modeHistoryTimelineDetail
	}
	return nil
}

func (m *Model) viewHistoryTimeline() []string {
	t := m.theme
	lines := []string{t.title.Render("VM timeline · " + m.timelineQuery), "", t.dim.Render("  changes only · a toggles unchanged observations")}
	if m.historyErr != nil {
		return append(lines, t.warn.Render("  "+m.historyErr.Error()))
	}
	if len(m.timeline) == 0 {
		return append(lines, t.dim.Render("  no timeline events"))
	}
	for i, e := range m.timeline {
		detail := ""
		if len(e.Changes) > 0 {
			parts := make([]string, len(e.Changes))
			for j, f := range e.Changes {
				parts[j] = f.Field + ":" + nonempty(f.Before, "—") + "→" + nonempty(f.After, "—")
			}
			detail = strings.Join(parts, " ")
		}
		line := fmt.Sprintf("  %-16s %-16s %-12s %s", e.Run.StartedAt.Local().Format("2006-01-02 15:04"), e.Kind, e.Context, truncate(detail, max(1, m.width-52)))
		if i == m.timelineCursor {
			line = t.focused.Render(line)
		} else {
			line = t.text.Render(line)
		}
		lines = append(lines, line)
	}
	return scrollLines(lines, 0, m.bodyHeight())
}

func (m *Model) viewHistoryTimelineDetail() []string {
	t := m.theme
	if m.timelineCursor < 0 || m.timelineCursor >= len(m.timeline) {
		return []string{t.dim.Render("nothing selected")}
	}
	e := m.timeline[m.timelineCursor]
	lines := []string{t.title.Render(e.Name), "", "  " + t.label.Render("Event") + "    " + e.Kind, "  " + t.label.Render("Run") + "      " + historyRunLabel(e.Run.ID), "  " + t.label.Render("Date") + "     " + e.Run.StartedAt.Local().Format("2006-01-02 15:04:05"), "  " + t.label.Render("Context") + "  " + e.Context}
	if len(e.Changes) > 0 {
		lines = append(lines, "", t.header.Render("Changes"))
		for _, f := range e.Changes {
			lines = append(lines, fmt.Sprintf("  %-18s %s → %s", f.Field, truncate(nonempty(f.Before, "—"), 24), truncate(nonempty(f.After, "—"), 24)))
		}
	}
	return scrollLines(lines, 0, m.bodyHeight())
}

func nonempty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func changeDetail(v assessment.VMChange) string {
	if len(v.Fields) == 0 {
		return ""
	}
	parts := make([]string, len(v.Fields))
	for i, f := range v.Fields {
		parts[i] = f.Field + ":" + nonempty(f.Before, "—") + "→" + nonempty(f.After, "—")
	}
	return strings.Join(parts, " ")
}

func historyRunLabel(id int64) string { return "#" + strconv.FormatInt(id, 10) }
