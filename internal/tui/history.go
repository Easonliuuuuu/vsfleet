package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/humanize"
)

func (m *Model) enterChanges() tea.Cmd {
	m.mode = modeChanges
	m.historyPane = historyPaneChanges
	m.filter.Placeholder = "filter changes"
	m.filtering = false
	m.filter.Blur()
	m.changeCursor, m.changeOffset = 0, 0
	if m.assessment == nil {
		m.historyErr = fmt.Errorf("historical assessments are unavailable")
		return nil
	}
	m.historyErr = nil
	return tea.Batch(loadHistoryRunsCmd(m.ctx, m.assessment), loadHistoryTrendsCmd(m.ctx, m.assessment))
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
		rows = append(rows, historyRow{kind: "vm", label: v.Name, context: v.Context, change: strings.Join(v.Changes, ", "), detail: changeDetail(v)})
	}
	for _, s := range m.changeDiff.Snapshots {
		rows = append(rows, historyRow{kind: "snapshot", label: s.VMName + " / " + s.Name, context: s.Context, change: "snapshot " + s.Kind, detail: nonempty(s.After, s.Before)})
	}
	for _, r := range m.changeDiff.Resources {
		rows = append(rows, historyRow{kind: r.Kind, label: r.Name, context: r.Context, change: strings.Join(r.Changes, ", "), detail: resourceDetail(r)})
	}
	needle := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if needle == "" {
		return rows
	}
	out := rows[:0]
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.label), needle) || strings.Contains(strings.ToLower(r.context), needle) || strings.Contains(strings.ToLower(r.change), needle) || strings.Contains(strings.ToLower(r.kind), needle) {
			out = append(out, r)
		}
	}
	return out
}

// historyRow is one line of the Changes list. kind names what changed — vm,
// snapshot, or an infrastructure kind (cluster, host, datastore) — as its own
// field so the CHANGE and KIND columns stop conflating "what happened" with
// "what it happened to".
type historyRow struct{ label, context, change, detail, kind string }

func (m *Model) viewChangesHeader() string {
	t := m.theme
	labels := []string{"Changes", "Trends", "Runs"}
	pane := m.historyPane
	if pane < 0 || pane >= len(labels) {
		pane = historyPaneChanges
	}
	// The run identities themselves now live on the comparison bar beneath
	// this header, so the header no longer restates "target #N" — a figure
	// the bar already shows by label, date, and coverage instead of a bare ID.
	label := "history  ·  " + labels[pane]
	return t.title.Render("vsfleet") + "  " + t.accent.Render(label) + t.dim.Render("   ←/→ switch")
}

// viewComparisonBar renders the baseline and target runs as a permanent,
// legible object: label, pin, id, when, status, and vCenter coverage for
// each, the gap between them, and — when the two runs did not cover the same
// vCenters — a plain-language line naming which ones were left out. This is
// the fix for the diff's loudest failure mode: a narrower capture reading as
// mass deletion because nothing on screen said the comparison was partial.
func (m *Model) viewComparisonBar(d *assessment.Diff) []string {
	t := m.theme
	lines := []string{
		"  " + t.accent.Render("b") + " " + t.dim.Render("baseline") + "  " + comparisonRunLine(t, d.Base, "", m.width),
		"  " + t.accent.Render("t") + " " + t.dim.Render("target ") + "  " + comparisonRunLine(t, d.Target, formatInterval(d.Target.StartedAt.Sub(d.Base.StartedAt)), m.width),
	}
	missingTarget, missingBaseline := coverageGaps(d)
	if len(missingTarget) > 0 {
		lines = append(lines, "  "+t.warn.Render(fmt.Sprintf("! not compared: %s — collected in the baseline but not the target", strings.Join(missingTarget, ", "))))
	}
	if len(missingBaseline) > 0 {
		lines = append(lines, "  "+t.warn.Render(fmt.Sprintf("! not compared: %s — collected in the target but not the baseline", strings.Join(missingBaseline, ", "))))
	}
	// Two runs can report the same VCenterID for genuinely different
	// vCenters (a simulator fixture, a cloned appliance) — Diff cannot name
	// the gap by vCenter in that case, but the counts still disagree and the
	// operator still needs to know the comparison is not full-estate.
	if len(missingTarget) == 0 && len(missingBaseline) == 0 && d.Base.SuccessfulContexts != d.Target.SuccessfulContexts {
		lines = append(lines, "  "+t.warn.Render(fmt.Sprintf("! baseline covered %s, target covered %s — the counts below are not a full-estate comparison", vCenterCount(d.Base.SuccessfulContexts), vCenterCount(d.Target.SuccessfulContexts))))
	}
	for _, note := range coverageNotes(d) {
		lines = append(lines, "  "+t.dim.Render("· "+note))
	}
	for i, line := range lines {
		lines[i] = truncate(line, m.width)
	}
	return lines
}

// comparisonRunLine formats one run's identity for the bar: its label (with
// a pin), id, local date/time, status, and how much of the estate it
// actually covered — the field this whole bar exists to make legible.
// interval is appended when non-empty — only the target row carries the gap
// since it reads relative to the baseline above it. Below 100 columns the
// weekday and label give way first, since coverage is the figure that must
// survive an 80 column terminal without an ellipsis.
func comparisonRunLine(t theme, r assessment.Run, interval string, width int) string {
	labelW := 20
	dateFormat := "Mon 02 Jan 15:04"
	if width < 100 {
		labelW = 14
		dateFormat = "01-02 15:04"
	}
	label := r.Label
	if label == "" {
		label = "(unlabelled)"
	}
	if r.Pinned {
		label = "📌 " + label
	}
	status := t.dim.Render(pad(string(r.Status), 8, false))
	switch r.Status {
	case assessment.RunComplete:
		status = t.ok.Render(pad(string(r.Status), 8, false))
	case assessment.RunPartial, assessment.RunFailed:
		status = t.warn.Render(pad(string(r.Status), 8, false))
	}
	coverage := vCenterCount(r.SuccessfulContexts)
	coverStyle := t.dim
	if r.RequestedContexts > 0 && r.SuccessfulContexts < r.RequestedContexts {
		coverage = fmt.Sprintf("%d/%d vCenters", r.SuccessfulContexts, r.RequestedContexts)
		coverStyle = t.warn
	}
	line := fmt.Sprintf("%s %-6s %s  %s  %s", pad(label, labelW, false), historyRunLabel(r.ID), r.StartedAt.Local().Format(dateFormat), status, coverStyle.Render(pad(coverage, 12, false)))
	if interval != "" {
		line += t.faint.Render(" +" + interval)
	}
	return line
}

// vCenterCount pluralizes a vCenter tally the way an operator would say it.
func vCenterCount(n int) string {
	if n == 1 {
		return "1 vCenter"
	}
	return fmt.Sprintf("%d vCenters", n)
}

// coverageGaps names, by human context name, which vCenters the diff could
// not compare on each side — everything Diff.Coverage recorded, whether the
// vCenter was missing entirely or its collection simply failed that run.
// Deduplicated because infrastructure coverage repeats one entry per kind
// (host, cluster, datastore) for the same context.
func coverageGaps(d *assessment.Diff) (missingFromTarget, missingFromBaseline []string) {
	seenTarget, seenBaseline := map[string]bool{}, map[string]bool{}
	for _, c := range d.Coverage {
		if c.Context == "" {
			continue
		}
		switch c.Scope {
		case "target":
			if !seenTarget[c.Context] {
				seenTarget[c.Context] = true
				missingFromTarget = append(missingFromTarget, c.Context)
			}
		case "baseline":
			if !seenBaseline[c.Context] {
				seenBaseline[c.Context] = true
				missingFromBaseline = append(missingFromBaseline, c.Context)
			}
		}
	}
	sort.Strings(missingFromTarget)
	sort.Strings(missingFromBaseline)
	return missingFromTarget, missingFromBaseline
}

// coverageNotes returns coverage issues that name no specific vCenter — a
// resource kind never recorded at all, typically because a run predates that
// collection existing. These do not fit the per-vCenter gap lines above, but
// dropping them silently would lose real information about what the diff
// could not check.
func coverageNotes(d *assessment.Diff) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range d.Coverage {
		if c.Context != "" || seen[c.Message] {
			continue
		}
		seen[c.Message] = true
		out = append(out, c.Message)
	}
	sort.Strings(out)
	return out
}

// formatInterval renders the gap between two runs the way an operator would
// say it out loud: minutes below an hour, hours and minutes below a day,
// whole days and hours beyond that.
func formatInterval(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		days := int(d.Hours()) / 24
		return fmt.Sprintf("%dd%dh", days, int(d.Hours())%24)
	}
}

// countSegment colors one figure in the counts line by what it means: dim
// when it is zero (nothing to draw the eye), the semantic style otherwise.
// This is what stops "-96 vanished" and "0 moved" from reading as equally
// important, which the old single dim-bold line did not distinguish.
func countSegment(t theme, n int, label string, style lipgloss.Style) string {
	if n == 0 {
		return t.faint.Render(fmt.Sprintf("%d %s", n, label))
	}
	return style.Render(fmt.Sprintf("%d %s", n, label))
}

func (m *Model) handleChangesKey(msg tea.KeyMsg) tea.Cmd {
	if m.historyPane != historyPaneChanges {
		switch {
		case key.Matches(msg, m.keys.PrevPane):
			m.historyPane = (m.historyPane + 2) % 3
			return nil
		case key.Matches(msg, m.keys.NextPane):
			m.historyPane = (m.historyPane + 1) % 3
			return nil
		case key.Matches(msg, m.keys.Capture):
			// Capture is the hub's primary action, so it answers from every
			// pane rather than only from Changes.
			return m.captureCommand()
		case key.Matches(msg, m.keys.Back):
			m.mode = modeBrowse
			m.historyPane = historyPaneChanges
			return nil
		case key.Matches(msg, m.keys.Up):
			if m.historyPane == historyPaneRuns {
				m.runCursor = clamp(m.runCursor-1, 0, max(0, len(m.runs)-1))
			}
			return nil
		case key.Matches(msg, m.keys.Down):
			if m.historyPane == historyPaneRuns {
				m.runCursor = clamp(m.runCursor+1, 0, max(0, len(m.runs)-1))
			}
			return nil
		case key.Matches(msg, m.keys.EditRun):
			if m.historyPane == historyPaneRuns {
				return m.beginRunEdit("label")
			}
		case key.Matches(msg, m.keys.NoteRun):
			if m.historyPane == historyPaneRuns {
				return m.beginRunEdit("note")
			}
		case key.Matches(msg, m.keys.PinRun):
			if m.historyPane == historyPaneRuns && len(m.runs) > 0 && m.assessment != nil {
				return toggleHistoryRunPinCmd(m.ctx, m.assessment, m.runs[clamp(m.runCursor, 0, len(m.runs)-1)])
			}
		}
		return nil
	}
	rows := m.changeRows()
	switch {
	case key.Matches(msg, m.keys.PrevPane):
		m.historyPane = historyPaneRuns
		return nil
	case key.Matches(msg, m.keys.NextPane):
		m.historyPane = historyPaneTrends
		return nil
	case key.Matches(msg, m.keys.History):
		m.mode = modeBrowse
		m.filter.Placeholder = filterPlaceholder
	case key.Matches(msg, m.keys.Back):
		m.mode = modeBrowse
		m.filter.Placeholder = filterPlaceholder
	case key.Matches(msg, m.keys.Timeline):
		if len(rows) == 0 || m.changeCursor >= len(rows) {
			return nil
		}
		return m.openTimelineForRow(rows[m.changeCursor], modeChanges)
	case key.Matches(msg, m.keys.Up):
		m.changeCursor = clamp(m.changeCursor-1, 0, max(0, len(rows)-1))
		m.scrollChangesIntoView(len(rows))
	case key.Matches(msg, m.keys.Down):
		m.changeCursor = clamp(m.changeCursor+1, 0, max(0, len(rows)-1))
		m.scrollChangesIntoView(len(rows))
	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		return m.filter.Focus()
	case key.Matches(msg, m.keys.Capture):
		return m.captureCommand()
	case key.Matches(msg, m.keys.Swap):
		if m.baseRun == 0 || m.targetRun == 0 {
			return nil
		}
		m.baseRun, m.targetRun = m.targetRun, m.baseRun
		return m.historyDiffCommand()
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
		// Above the split threshold the inspector is already showing beside
		// the list and follows the cursor, so there is nothing left for
		// Enter to open — that is the whole point of the split.
		if len(rows) > 0 && m.changeCursor < len(rows) && m.historySplitWidth() == 0 {
			m.mode = modeChangeDetail
		}
	}
	return nil
}

// openTimelineForRow opens the VM timeline for one Changes row, remembering
// from so Esc on the timeline returns to whichever screen — the Changes
// pane, or its narrow-terminal change-detail fallback — actually opened it.
func (m *Model) openTimelineForRow(row historyRow, from mode) tea.Cmd {
	if m.assessment == nil {
		return nil
	}
	query := row.label
	if i := strings.Index(query, " / "); i >= 0 {
		query = query[:i]
	}
	m.timelineQuery = query
	m.timelineAll = false
	m.timelineCursor, m.timelineOffset = 0, 0
	m.historyErr = nil
	m.timelineFrom = from
	m.mode = modeHistoryTimeline
	return loadHistoryTimelineCmd(m.ctx, m.assessment, query, false, false)
}

// handleChangeDetailKey drives the narrow-terminal change-detail fallback.
// Up/Down move the underlying cursor and re-render this row's detail without
// leaving the mode, so paging through changes below the split threshold does
// not mean back, down, enter, repeat. Timeline opens that VM's full history —
// previously advertised in the footer here but silently dropped by the
// dispatch, so pressing it did nothing.
func (m *Model) handleChangeDetailKey(msg tea.KeyMsg) tea.Cmd {
	rows := m.changeRows()
	switch {
	case key.Matches(msg, m.keys.Back):
		m.mode = modeChanges
	case key.Matches(msg, m.keys.Up):
		m.changeCursor = clamp(m.changeCursor-1, 0, max(0, len(rows)-1))
		m.scrollChangesIntoView(len(rows))
	case key.Matches(msg, m.keys.Down):
		m.changeCursor = clamp(m.changeCursor+1, 0, max(0, len(rows)-1))
		m.scrollChangesIntoView(len(rows))
	case key.Matches(msg, m.keys.Timeline):
		if len(rows) == 0 || m.changeCursor >= len(rows) {
			return nil
		}
		return m.openTimelineForRow(rows[m.changeCursor], modeChangeDetail)
	}
	return nil
}

// captureContexts is what a capture reads: the vCenters in scope, not every
// vCenter configured. The all-vCenters view is how an estate-wide capture is
// asked for; with one vCenter on screen, "n" must not connect to the others or
// ask for their passwords.
func (m *Model) captureContexts() []*config.Context {
	states := m.inScope()
	contexts := make([]*config.Context, 0, len(states))
	for _, st := range states {
		contexts = append(contexts, st.cc)
	}
	return contexts
}

// captureCommand starts a capture across the vCenter(s) in scope, ignoring
// the keypress when history is unavailable, a capture is already running, or
// scope is empty.
func (m *Model) captureCommand() tea.Cmd {
	if m.assessment == nil || m.capturing {
		return nil
	}
	states := m.inScope()
	if len(states) == 0 {
		m.historyErr = fmt.Errorf("no vCenter in scope to capture")
		return nil
	}
	m.capturing = true
	m.historyErr = nil
	for _, st := range states {
		st.capturing = true
	}
	m.setMessage(captureScopeMessage(states), false)
	return captureHistoryCmd(m.ctx, m.assessment, m.captureContexts())
}

// captureScopeLabel names a set of contexts: the single vCenter's name, or a
// count when there are several — the all-vCenters view is the only way to
// reach more than one.
func captureScopeLabel(states []*contextState) string {
	if len(states) == 1 {
		return states[0].cc.Name
	}
	return fmt.Sprintf("%d vCenters", len(states))
}

// captureScopeMessage is the status line shown the moment "n" is pressed.
func captureScopeMessage(states []*contextState) string {
	return "capturing " + captureScopeLabel(states) + "…"
}

// capturingStates is the vCenter(s) a running capture actually covers, fixed
// at the moment "n" was pressed. The operator's current scope may have moved
// on since (e.g. narrowing back from the all-vCenters view), so the in-flight
// message reports what was captured, not what is on screen now.
func (m *Model) capturingStates() []*contextState {
	var states []*contextState
	for _, st := range m.states {
		if st.capturing {
			states = append(states, st)
		}
	}
	return states
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
	if m.historyPane == historyPaneTrends {
		return m.viewHistoryTrends()
	}
	if m.historyPane == historyPaneRuns {
		return m.viewHistoryHubRuns()
	}
	t := m.theme
	lines := []string{t.title.Render("Changes"), ""}
	if m.capturing {
		lines = append(lines, "  "+m.spin.View()+t.dim.Render("capturing VM inventory from "+captureScopeLabel(m.capturingStates())+"…"), "")
	}
	if m.historyErr != nil {
		return append(lines, "  "+t.warn.Render(m.historyErr.Error()), t.dim.Render("  press n to capture "+captureScopeLabel(m.inScope())))
	}
	if m.changeDiff == nil {
		return append(lines, t.dim.Render("no comparable assessments"))
	}
	d := m.changeDiff
	lines = append(lines, m.viewComparisonBar(d)...)
	lines = append(lines, "")
	c := d.Counts
	counts := strings.Join([]string{
		countSegment(t, c.Vanished, "vanished", t.bad),
		countSegment(t, c.Appeared, "appeared", t.ok),
		countSegment(t, c.Moved, "moved", t.warn),
		countSegment(t, c.Modified, "modified", t.warn),
		countSegment(t, c.Resources, "infra", t.warn),
		countSegment(t, c.Snapshots, "snapshots", t.warn),
	}, t.faint.Render("   "))
	lines = append(lines, truncate("  "+counts, m.width), "")
	rows := m.changeRows()
	if len(rows) == 0 {
		lines = append(lines, t.dim.Render("no changes in the selected assessments"))
		return lines
	}
	avail := m.changesListHeight()
	// Above the split threshold, the inspector that used to be a whole
	// separate screen — Enter, look, Esc, repeat — now sits beside the list
	// and follows the cursor. Below it, the list keeps the full width it has
	// always had and Enter still opens a full-screen inspector.
	if splitW := m.historySplitWidth(); splitW > 0 {
		rightW := m.width - splitW - 3
		list := m.renderChangeList(rows, splitW, avail)
		var inspector []string
		if m.changeCursor >= 0 && m.changeCursor < len(rows) {
			inspector = m.historyInspector(rows[m.changeCursor])
		}
		return append(lines, joinSideBySide(t, list, inspector, splitW, rightW, avail)...)
	}
	return append(lines, m.renderChangeList(rows, m.width, avail)...)
}

// changesListHeight is how many rows renderChangeList can draw — its own
// heading included — below the title, comparison bar, and counts line. It is
// factored out of viewChanges so cursor movement can scroll the list into
// view using the exact budget the view renders with, the way scrollIntoView
// does for the browse table; without it, Up/Down could move the cursor past
// what renderChangeList draws with no way to see where it went, silencing
// the highlight the split's inspector otherwise gives no other cue for.
func (m *Model) changesListHeight() int {
	lines := 2 // title, blank
	if m.capturing {
		lines += 2
	}
	if m.changeDiff != nil {
		lines += len(m.viewComparisonBar(m.changeDiff)) + 3 // bar, blank, counts, blank
	}
	return max(1, m.bodyHeight()-lines)
}

// scrollChangesIntoView keeps changeCursor's row inside the window
// renderChangeList draws, the same job scrollIntoView does for the browse
// table's m.offset.
func (m *Model) scrollChangesIntoView(n int) {
	h := m.changesListHeight() - 1 // the list's own heading claims one row
	if h <= 0 {
		return
	}
	if m.changeCursor < m.changeOffset {
		m.changeOffset = m.changeCursor
	}
	if m.changeCursor >= m.changeOffset+h {
		m.changeOffset = m.changeCursor - h + 1
	}
	m.changeOffset = clamp(m.changeOffset, 0, max(0, n-h))
}

// historySplitWidth is the left column's width once the terminal is wide
// enough to show the inspector beside the list instead of behind a
// full-screen mode; 0 means the split does not apply at this width.
func (m *Model) historySplitWidth() int {
	const minSplitWidth = 92
	if m.width < minSplitWidth {
		return 0
	}
	w := m.width * 42 / 100
	if w < 34 {
		w = 34
	}
	if w > 52 {
		w = 52
	}
	return w
}

// renderChangeList renders the heading plus as many rows as fit height,
// starting from m.changeOffset, at the given width. It is used both for the
// single-pane list and for the split layout's narrower left column.
func (m *Model) renderChangeList(rows []historyRow, width, height int) []string {
	t := m.theme
	changeW, nameW, kindW, ctxW, detailW := historyColumnWidths(width)
	heading := "  " + pad("CHANGE", changeW, false) + " " + pad("NAME", nameW, false) + " " + pad("KIND", kindW, false) + " " + pad("vCENTER", ctxW, false)
	if detailW > 0 {
		heading += " " + pad("DETAIL", detailW, false)
	}
	lines := []string{t.header.Render(truncate(heading, width))}
	for i := m.changeOffset; i < len(rows) && len(lines) < height; i++ {
		r := rows[i]
		line := "  " + pad(r.change, changeW, false) + " " + pad(r.label, nameW, false) + " " + pad(r.kind, kindW, false) + " " + pad(r.context, ctxW, false)
		if detailW > 0 {
			line += " " + pad(r.detail, detailW, false)
		}
		line = truncate(line, width)
		if i == m.changeCursor {
			line = t.focused.Render(line)
		} else {
			line = t.text.Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}

// joinSideBySide pads two blocks of lines to fixed widths and joins them
// with a vertical rule, for the wide-terminal split between the Changes list
// and its inspector.
func joinSideBySide(t theme, left, right []string, leftW, rightW, height int) []string {
	rule := t.rule.Render("│")
	out := make([]string, height)
	for i := 0; i < height; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = pad(l, leftW, false) + " " + rule + " " + pad(r, rightW, false)
	}
	return out
}

// historyColumnWidths sizes the Changes list to the terminal: CHANGE, KIND
// and vCENTER hold their content at any reasonable width, NAME takes what is
// left up to a readable cap, and DETAIL — the per-row before/after preview —
// only appears once there is genuine room for it rather than truncating
// everything else to force it in.
func historyColumnWidths(width int) (changeW, nameW, kindW, ctxW, detailW int) {
	changeW, kindW, ctxW = 17, 10, 12
	if width < 70 {
		// Below single-pane comfort — also what the split layout's narrower
		// list column always lands in — CHANGE/KIND/vCENTER give up their
		// full-word room first, since NAME is what an operator scans for.
		changeW, kindW, ctxW = 10, 8, 10
	}
	avail := width - 2 - changeW - 1 - kindW - 1 - ctxW - 1
	nameW = avail
	if nameW > 34 {
		detailW = nameW - 34 - 1
		nameW = 34
	}
	if nameW < 8 {
		nameW = 8
	}
	if detailW < 10 {
		detailW = 0
	}
	return
}

func (m *Model) viewHistoryHubRuns() []string {
	t := m.theme
	lines := []string{t.title.Render("Runs"), "", t.dim.Render("  newest first · e label · N note · p pin · n capture")}
	for i, r := range m.runs {
		label := r.Label
		if label == "" {
			label = "—"
		}
		if r.Pinned {
			label = "📌 " + label
		}
		line := fmt.Sprintf("  %-5s %-18s %-9s %s", historyRunLabel(r.ID), truncate(label, 18), r.Status, r.StartedAt.Local().Format("2006-01-02 15:04"))
		if i == m.runCursor {
			line = t.focused.Render(line)
		} else {
			line = t.text.Render(line)
		}
		lines = append(lines, line)
	}
	if len(m.runs) == 0 {
		lines = append(lines, t.dim.Render("  no assessments stored"))
	}
	return scrollLines(lines, 0, m.bodyHeight())
}

func newRunEditInput() textinput.Model {
	input := textinput.New()
	input.CharLimit = 256
	input.Prompt = ""
	return input
}

func (m *Model) beginRunEdit(field string) tea.Cmd {
	if m.assessment == nil || len(m.runs) == 0 {
		return nil
	}
	run := m.runs[clamp(m.runCursor, 0, len(m.runs)-1)]
	m.runEditRunID, m.runEditKind = run.ID, field
	value := run.Label
	if field == "note" {
		value = run.Note
	}
	m.runEditInput.SetValue(value)
	m.runEditInput.Placeholder = "empty clears " + field
	m.runEditInput.Focus()
	m.mode = modeHistoryRunEdit
	m.historyErr = nil
	return nil
}

func (m *Model) handleHistoryRunEditKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.runEditInput.Blur()
		m.runEditKind, m.runEditRunID = "", 0
		m.mode = modeChanges
		return nil
	case tea.KeyEnter:
		if m.assessment == nil || m.runEditRunID == 0 {
			return nil
		}
		return updateHistoryRunCmd(m.ctx, m.assessment, m.runEditRunID, m.runEditKind, m.runEditInput.Value())
	}
	var cmd tea.Cmd
	m.runEditInput, cmd = m.runEditInput.Update(msg)
	return cmd
}

func (m *Model) viewHistoryRunEdit() []string {
	t := m.theme
	field := m.runEditKind
	if field == "" {
		field = "metadata"
	}
	return []string{t.title.Render("Edit run " + field), "", "  " + t.dim.Render("enter saves · esc cancels"), "", "  " + m.runEditInput.View()}
}

func (m *Model) viewHistoryTrends() []string {
	t := m.theme
	lines := []string{t.title.Render("Trends"), "", t.dim.Render("  last 30 complete assessments · ↑/↓ details")}
	if m.historyErr != nil {
		return append(lines, t.warn.Render("  "+m.historyErr.Error()))
	}
	if m.historyChurn == nil || m.historySnapshots == nil {
		return append(lines, t.dim.Render("  loading history trends…"))
	}
	var vmValues, snapshotValues []float64
	for _, p := range m.historyChurn.Points {
		vmValues = append(vmValues, float64(p.VMCount))
	}
	for _, p := range m.historySnapshots.Points {
		snapshotValues = append(snapshotValues, float64(p.Total))
	}
	lines = append(lines,
		fmt.Sprintf("  VMs       %s", tuiSparkline(vmValues)),
		fmt.Sprintf("  snapshots %s", tuiSparkline(snapshotValues)),
		"",
		t.header.Render("  DATE        VMs   +  -  →  ~   snapshots  stale"),
	)
	if m.historyCapacity != nil {
		for _, series := range m.historyCapacity.Series {
			if series.Scope != "estate" || len(series.Points) == 0 {
				continue
			}
			point := series.Points[len(series.Points)-1]
			value := "—"
			if point.StorageCapacity != nil {
				value = fmt.Sprintf("storage %.0f", *point.StorageCapacity)
			} else if point.CPUCapacity != nil {
				value = fmt.Sprintf("cpu %.0f", *point.CPUCapacity)
			}
			lines = append(lines, fmt.Sprintf("  %-8s %s", series.Kind, value))
		}
	}
	for i, p := range m.historyChurn.Points {
		sp := assessment.SnapshotTrendPoint{}
		if m.historySnapshots != nil && i < len(m.historySnapshots.Points) {
			sp = m.historySnapshots.Points[i]
		}
		lines = append(lines, fmt.Sprintf("  %-10s  %-4d %2d %2d %2d %2d      %-4d      %-4d", p.Run.StartedAt.Local().Format("2006-01-02"), p.VMCount, p.Appeared, p.Vanished, p.Moved, p.Modified, sp.Total, sp.Stale))
	}
	return scrollLines(lines, 0, m.bodyHeight())
}

func tuiSparkline(values []float64) string {
	if len(values) == 0 {
		return "—"
	}
	glyphs := []rune("▁▂▃▄▅▆▇█")
	min, max := values[0], values[0]
	for _, value := range values[1:] {
		if value < min {
			min = value
		}
		if value > max {
			max = value
		}
	}
	if min == max {
		return strings.Repeat(string(glyphs[0]), len(values))
	}
	var out strings.Builder
	for _, value := range values {
		i := int((value - min) / (max - min) * float64(len(glyphs)-1))
		out.WriteRune(glyphs[i])
	}
	return out.String()
}

// viewChangeDetail is the narrow-terminal fallback for a row's detail, below
// the width historySplitWidth needs to show the inspector beside the list.
// It renders the same content historyInspector puts in the split's right
// column, full width and full height, reached by Enter instead of always on.
func (m *Model) viewChangeDetail() []string {
	rows := m.changeRows()
	if m.changeCursor < 0 || m.changeCursor >= len(rows) {
		return []string{m.theme.dim.Render("nothing selected")}
	}
	return scrollLines(m.historyInspector(rows[m.changeCursor]), 0, m.bodyHeight())
}

// historyInspector is the detail for one Changes row: the summary already on
// the row, then whatever the diff's own Before/After observations add. This
// is what the old change-detail screen was missing for appeared and vanished
// rows — 97 of 110 changes in the scenario that motivated this screen's
// redesign — because it read only the row's flattened summary and never the
// full observation the diff had already loaded.
func (m *Model) historyInspector(r historyRow) []string {
	t := m.theme
	lines := []string{
		t.title.Render(r.label),
		"  " + t.label.Render("context") + "  " + r.context,
		"  " + t.label.Render("kind") + "     " + r.kind,
		"  " + t.label.Render("change") + "   " + r.change,
		"",
	}
	d := m.changeDiff
	if d == nil {
		return lines
	}
	switch r.kind {
	case "vm":
		for _, v := range d.VMs {
			if v.Name == r.label && v.Context == r.context {
				lines = append(lines, m.vmInspectorFields(v)...)
				break
			}
		}
	case "snapshot":
		if r.detail != "" {
			for _, line := range wrap(r.detail, m.width-4) {
				lines = append(lines, "  "+t.value.Render(line))
			}
		}
	default:
		for _, resource := range d.Resources {
			if resource.Kind == r.kind && resource.Name == r.label && resource.Context == r.context {
				lines = append(lines, resourceInspectorFields(t, resource)...)
				break
			}
		}
	}
	return lines
}

// vmInspectorFields renders a VM's current state — from After when the VM
// still exists, from Before when it vanished — plus the field-level changes
// a modified or moved VM carries. "Current state" is dated to whichever run
// the observation came from, since a vanished VM's last-known state belongs
// to the baseline, not the target.
func (m *Model) vmInspectorFields(v assessment.VMChange) []string {
	t := m.theme
	d := m.changeDiff
	obs, run := v.After, d.Target
	if obs == nil {
		obs, run = v.Before, d.Base
	}
	if obs == nil {
		return nil
	}
	vm := obs.VM
	lines := []string{
		t.header.Render("  State"),
		fmt.Sprintf("  %-12s %s · %s", "as of", historyRunLabel(run.ID), run.StartedAt.Local().Format("2006-01-02 15:04")),
		fmt.Sprintf("  %-12s %s", "power", nonempty(vm.PowerState, "—")),
		fmt.Sprintf("  %-12s %s", "host", nonempty(vm.Host, "—")),
		fmt.Sprintf("  %-12s %s", "cluster", nonempty(vm.Cluster, "—")),
		fmt.Sprintf("  %-12s %d vCPU · %s", "size", vm.CPU, humanize.MB(vm.MemoryMB)),
		fmt.Sprintf("  %-12s %s", "datastore", nonempty(strings.Join(vm.Datastores, ", "), "—")),
		fmt.Sprintf("  %-12s %s", "ip address", nonempty(vm.IPAddress, "—")),
		fmt.Sprintf("  %-12s %s", "guest os", nonempty(vm.GuestOS, "—")),
		fmt.Sprintf("  %-12s %s", "uuid", nonempty(vm.InstanceUUID, "—")),
	}
	if v.MatchBasis != "" {
		lines = append(lines, fmt.Sprintf("  %-12s %s", "matched by", v.MatchBasis))
	}
	if len(v.Fields) > 0 {
		lines = append(lines, "", t.header.Render("  Field changes"))
		for _, f := range v.Fields {
			lines = append(lines, fmt.Sprintf("  %-18s %s → %s", f.Field, truncate(nonempty(f.Before, "—"), 24), truncate(nonempty(f.After, "—"), 24)))
		}
	}
	return lines
}

func resourceInspectorFields(t theme, r assessment.ResourceChange) []string {
	if len(r.Fields) == 0 {
		return nil
	}
	lines := []string{t.header.Render("  Field changes")}
	for _, field := range r.Fields {
		lines = append(lines, fmt.Sprintf("  %-22s %s → %s", field.Field, truncate(nonempty(field.Before, "—"), 24), truncate(nonempty(field.After, "—"), 24)))
	}
	return lines
}

func resourceDetail(r assessment.ResourceChange) string {
	if len(r.Fields) == 0 {
		return ""
	}
	parts := make([]string, len(r.Fields))
	for i, field := range r.Fields {
		parts[i] = field.Field + ":" + field.Before + "→" + field.After
	}
	return strings.Join(parts, " ")
}

func (m *Model) handleHistoryTimelineKey(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, m.keys.Back) {
		m.mode = m.timelineFrom
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
