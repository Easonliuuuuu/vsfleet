package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/easonliuuuuu/vsfleet/internal/health"
)

// switchHistoryPane shows another History pane. The filter belongs to the
// pane it was typed in, Changes or Health, so it does not follow the
// operator to the other one.
func (m *Model) switchHistoryPane(pane int) {
	if pane != m.historyPane && (m.historyPane == historyPaneHealth || pane == historyPaneHealth) {
		m.filter.SetValue("")
	}
	m.historyPane = pane
	m.hubOffset = 0
}

// historyFilterHint is the filter line's count in History: how many of the
// pane's changes or findings the query keeps.
func (m *Model) historyFilterHint() string {
	var hint string
	if m.historyPane == historyPaneHealth && m.historyHealth != nil {
		hint = fmt.Sprintf("  %d of %d findings", len(m.healthRows()), len(m.historyHealth.Findings))
	} else {
		hint = fmt.Sprintf("  %d %s match", len(m.scopeRows()), plural("row", len(m.scopeRows())))
	}
	if !m.filtering {
		hint += " · esc clears"
	}
	return hint
}

// healthDetailLabelWidth is the field-name gutter of a finding's detail.
const healthDetailLabelWidth = 11

func (m *Model) viewHistoryHealth() []string {
	t := m.theme
	// Health is not scoped: its rules run over the whole stored assessment, so
	// the header says so rather than letting it be read as the selected
	// vCenter's verdict.
	head := []string{t.title.Render("Health"), "", truncate(t.dim.Render("  all vCenters · read-only findings from the latest stored assessment"), m.width)}
	if m.historyHealthErr != nil {
		return append(head, "  "+t.warn.Render(m.historyHealthErr.Error()))
	}
	if m.historyHealth == nil {
		return append(head, t.dim.Render("  loading health findings…"))
	}
	r := m.historyHealth
	rows := m.healthRows()
	if m.healthDetail && len(rows) > 0 {
		return m.viewHealthDetail(rows[clamp(m.healthCursor, 0, len(rows)-1)])
	}
	readiness := health.Readiness(*r)
	// Critical leads the counts and the total comes last. The line is cut at
	// the terminal width, and on an 80 column terminal the figure that goes
	// missing must be the least actionable one, never the critical count.
	c := r.Counts
	sep := t.faint.Render(" · ")
	summary := "  assessment " + historyRunLabel(r.RunID) + "  " + strings.ToUpper(readiness.Verdict) + sep +
		countSegment(t, c.Critical, "critical", t.bad) + sep +
		countSegment(t, c.Warning, "warning", t.warn) + sep +
		countSegment(t, c.Info, "info", t.dim) + sep +
		t.faint.Render(fmt.Sprintf("%d total", c.Total))
	head = append(head, truncate(summary, m.width))
	// What the findings could not see belongs beside the verdict it
	// qualifies, not under a thousand rows nobody scrolls past.
	if line := m.healthCoverageLine(); line != "" {
		head = append(head, truncate("  "+t.warn.Render(line), m.width))
	}
	head = append(head, "")
	// The summary and column header stay put and only the findings scroll,
	// so a row two screens down still says which column is which.
	var body []string
	if len(r.Findings) == 0 {
		body = append(body, t.ok.Render("  no findings"))
	} else if len(rows) == 0 {
		body = append(body, t.dim.Render("  no findings match "+fmt.Sprintf("%q", strings.TrimSpace(m.filter.Value()))+" — esc clears the filter"))
	} else {
		cols := healthColumns(rows, m.width)
		titles := make([]string, 0, len(cols))
		for _, col := range cols {
			titles = append(titles, pad(col.title, col.width, false))
		}
		head = append(head, t.header.Render(truncate("  "+strings.Join(titles, healthGap), m.width)))
		for i, finding := range rows {
			cells := make([]string, 0, len(cols))
			for _, col := range cols {
				cells = append(cells, pad(col.cell(finding), col.width, false))
			}
			style := t.warn
			if finding.Severity == health.SeverityCritical {
				style = t.bad
			} else if finding.Severity == health.SeverityInfo {
				style = t.dim
			}
			if i == m.healthCursor {
				style = t.focused
			}
			body = append(body, style.Render(truncate(listCursor(i == m.healthCursor)+" "+strings.Join(cells, healthGap), m.width)))
		}
	}
	if strings.TrimSpace(m.filter.Value()) == "" {
		body = append(body, m.healthRuleNotes()...)
	}
	h := m.bodyHeight() - len(head)
	m.healthListHeight = h
	m.keepHealthCursorInView(len(rows), len(body), h)
	return append(head, m.scrollHubLines(body, h)...)
}

// healthRows is the findings the pane lists: critical first, narrowed by the
// filter to those whose severity, category, rule, vCenter, object or message
// contains it.
func (m *Model) healthRows() []health.Finding {
	if m.historyHealth == nil {
		return nil
	}
	rows := sortFindingsBySeverity(m.historyHealth.Findings)
	needle := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if needle == "" {
		return rows
	}
	kept := rows[:0]
	for _, f := range rows {
		hay := strings.ToLower(strings.Join([]string{string(f.Severity), string(f.Category), f.Rule, f.Object.Context, f.Object.Kind + "/" + f.Object.Name, f.Message}, "\x00"))
		if strings.Contains(hay, needle) {
			kept = append(kept, f)
		}
	}
	return kept
}

// healthCoverageLine names what the latest assessment could not judge: the
// vCenters it did not collect and the rules it could not, or could only
// partly, evaluate. The rules themselves are listed under the findings.
func (m *Model) healthCoverageLine() string {
	var parts []string
	if missed := m.uncollectedVCenters(); len(missed) > 0 {
		parts = append(parts, "not collected: "+strings.Join(missed, ", "))
	}
	notEvaluated, partly := 0, 0
	for _, rule := range m.historyHealth.Rules {
		if rule.Status == "not-evaluated" || rule.Result == "unknown" {
			notEvaluated++
		} else if len(rule.Partial) > 0 {
			partly++
		}
	}
	if notEvaluated > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", notEvaluated, plural("rule", notEvaluated))+" not evaluated")
	}
	if partly > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", partly, plural("rule", partly))+" partly evaluated")
	}
	if len(parts) == 0 {
		return ""
	}
	line := strings.Join(parts, " · ")
	if notEvaluated+partly > 0 {
		line += " — listed after the findings"
	}
	return line
}

// uncollectedVCenters is every vCenter the assessment Health reads tried and
// failed to collect.
func (m *Model) uncollectedVCenters() []string {
	var missed []string
	for name, status := range m.historyCoverage[m.historyHealth.RunID] {
		if status != "success" {
			missed = append(missed, name)
		}
	}
	sort.Strings(missed)
	return missed
}

// healthRuleNotes says which rules were not, or only partly, evaluated, and
// why.
func (m *Model) healthRuleNotes() []string {
	t := m.theme
	var notes []string
	for _, rule := range m.historyHealth.Rules {
		if rule.Status == "not-evaluated" || rule.Result == "unknown" {
			reason := rule.Reason
			if len(rule.Blind) > 0 {
				reason = strings.TrimSpace(strings.TrimSuffix(reason, ".") + "; blind contexts: " + strings.Join(rule.Blind, ", "))
			}
			notes = append(notes, t.dim.Render("  not evaluated: "+rule.Rule+" — "+reason))
		} else if len(rule.Partial) > 0 {
			notes = append(notes, t.dim.Render("  partly evaluated: "+rule.Rule+" — "+rule.Reason))
		}
	}
	if len(notes) > 0 {
		notes = append([]string{""}, notes...)
	}
	return notes
}

// keepHealthCursorInView scrolls the findings so the cursor row is on
// screen. On the last finding it scrolls to the end, so the rule notes after
// the findings can be read.
func (m *Model) keepHealthCursorInView(rows, lines, h int) {
	if h <= 0 || rows == 0 {
		return
	}
	m.healthCursor = clamp(m.healthCursor, 0, rows-1)
	switch {
	case m.healthCursor == rows-1:
		m.hubOffset = max(0, lines-h)
	case m.healthCursor < m.hubOffset:
		m.hubOffset = m.healthCursor
	case m.healthCursor >= m.hubOffset+h:
		m.hubOffset = m.healthCursor - h + 1
	}
}

// handleHealthKey moves the findings cursor, opens and closes a finding's
// detail and starts the filter. It reports whether the key was one of those.
func (m *Model) handleHealthKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if m.healthDetail {
		switch {
		case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Open):
			m.healthDetail = false
			m.hubOffset = 0
			return nil, true
		}
		// The detail scrolls like the other hub panes.
		return nil, m.scrollHubPane(msg)
	}
	n := len(m.healthRows())
	page := max(1, m.healthListHeight-1)
	switch {
	case key.Matches(msg, m.keys.Up):
		m.healthCursor--
	case key.Matches(msg, m.keys.Down):
		m.healthCursor++
	case key.Matches(msg, m.keys.PageUp):
		m.healthCursor -= page
	case key.Matches(msg, m.keys.PageDown):
		m.healthCursor += page
	case key.Matches(msg, m.keys.Home):
		m.healthCursor = 0
	case key.Matches(msg, m.keys.End):
		m.healthCursor = n - 1
	case key.Matches(msg, m.keys.Open):
		if n > 0 {
			m.healthDetail = true
			m.hubOffset = 0
		}
	case key.Matches(msg, m.keys.Filter):
		m.filter.Placeholder = "filter findings"
		m.filtering = true
		m.healthCursor = 0
		return m.filter.Focus(), true
	case key.Matches(msg, m.keys.Back) && m.filter.Value() != "":
		// Esc clears a filter before it leaves History, the way it does in
		// the browse table.
		m.filter.SetValue("")
		m.healthCursor = 0
	default:
		return nil, false
	}
	m.healthCursor = clamp(m.healthCursor, 0, max(0, n-1))
	return nil, true
}

// viewHealthDetail is one finding in full: what it is about and where, the
// whole message, the evidence behind it, what to do, and the rule that
// raised it.
func (m *Model) viewHealthDetail(f health.Finding) []string {
	t := m.theme
	w := max(20, m.width-2-healthDetailLabelWidth)
	var body []string
	field := func(name, value string) {
		for i, line := range wrap(value, w) {
			label := ""
			if i == 0 {
				label = name
			}
			body = append(body, "  "+t.dim.Render(pad(label, healthDetailLabelWidth, false))+line)
		}
	}
	field("OBJECT", f.Object.Kind+" "+f.Object.Name)
	where := nonempty(f.Object.Context, "—")
	if f.Object.Datacenter != "" {
		where += " · datacenter " + f.Object.Datacenter
	}
	field("VCENTER", where)
	if f.Object.ID != "" {
		field("ID", f.Object.ID)
	}
	field("CATEGORY", string(f.Category))
	if f.Confidence != "" {
		field("CONFIDENCE", string(f.Confidence))
	}
	field("MESSAGE", f.Message)
	for i, e := range f.Evidence {
		label := ""
		if i == 0 {
			label = "EVIDENCE"
		}
		value := e.Field + " " + e.Observed
		if e.Expected != "" {
			value += " · expected " + e.Expected
		}
		field(label, value)
	}
	if f.Recommendation != "" {
		field("FIX", f.Recommendation)
	}
	rule := f.Rule
	for _, r := range health.Rules() {
		if r.ID == f.Rule && r.Summary != "" {
			rule += " — raised when " + r.Summary
		}
	}
	field("RULE", rule)
	style := t.warn
	switch f.Severity {
	case health.SeverityCritical:
		style = t.bad
	case health.SeverityInfo:
		style = t.dim
	}
	head := []string{t.title.Render("Health") + t.dim.Render(" › ") + style.Render(string(f.Severity)) + t.dim.Render(" · "+f.Object.Name), ""}
	return append(head, m.scrollHubLines(body, m.bodyHeight()-len(head))...)
}

// sortFindingsBySeverity lists critical findings first. The stored order is
// by rule, which on a real estate puts hundreds of informational findings in
// front of the handful that block a migration, and the pane scrolls: the
// critical rows were off the bottom of the screen. The sort is stable, so
// within a severity the report's own order is kept and the list does not
// shuffle between renders.
func sortFindingsBySeverity(in []health.Finding) []health.Finding {
	out := append([]health.Finding(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		return findingSeverityRank(out[i].Severity) > findingSeverityRank(out[j].Severity)
	})
	return out
}

func findingSeverityRank(s health.Severity) int {
	switch s {
	case health.SeverityCritical:
		return 2
	case health.SeverityWarning:
		return 1
	default:
		return 0
	}
}

// healthColumn is one column of the findings table: its heading, how to read
// it off a finding, and the width it is drawn at.
type healthColumn struct {
	title string
	width int
	cell  func(health.Finding) string
}

// healthGap separates the findings columns by the same gap every other table
// uses; a single space ran SEVERITY into RULE ("critical datastore-…").
var healthGap = strings.Repeat(" ", cellGap)

// healthMessageMin is the least width the MESSAGE column is given before
// lower-value columns are dropped to make room. The message is what tells the
// operator what to do, so it is worth more than CATEGORY or CONFIDENCE.
const healthMessageMin = 24

// healthMessageSqueezed is the floor once RULE and OBJECT are being
// shortened as well: below it a message is too clipped to read. It gave up
// three columns when the findings columns moved from a one space gap to the
// shared cellGap, so that an 80 column terminal still shows the whole of a
// 25 column OBJECT such as "datastore/vsan-archive-01".
const healthMessageSqueezed = 17

// healthColumns sizes the findings table to the data and the terminal.
// Columns take the width of their longest value rather than a fixed
// allotment, and a column with no values at all is not drawn: CONFIDENCE is
// only ever set by the orphan-VMDK rule, and reserving 26 columns for it
// pushed OBJECT off an 80 column screen. When even that is too wide,
// CONFIDENCE goes first, then CATEGORY, then VCENTER, and last RULE and
// OBJECT are shortened. OBJECT is never dropped: a finding that does not say what it is
// about cannot be acted on.
func healthColumns(findings []health.Finding, width int) []healthColumn {
	sev := healthColumn{"SEVERITY", 8, func(f health.Finding) string { return string(f.Severity) }}
	conf := healthColumn{title: "CONFIDENCE", cell: func(f health.Finding) string { return string(f.Confidence) }}
	cat := healthColumn{title: "CATEGORY", cell: func(f health.Finding) string { return string(f.Category) }}
	rule := healthColumn{title: "RULE", cell: func(f health.Finding) string { return f.Rule }}
	vc := healthColumn{title: "VCENTER", cell: func(f health.Finding) string { return f.Object.Context }}
	obj := healthColumn{title: "OBJECT", cell: func(f health.Finding) string { return f.Object.Kind + "/" + f.Object.Name }}
	msg := healthColumn{"MESSAGE", 0, func(f health.Finding) string { return f.Message }}

	// fit sets a column to the width of its longest value, never wider than
	// limit, and reports whether any finding had a value at all.
	fit := func(c *healthColumn, limit int) bool {
		c.width = ansi.StringWidth(c.title)
		found := false
		for _, f := range findings {
			if v := c.cell(f); v != "" {
				found = true
				c.width = max(c.width, ansi.StringWidth(v))
			}
		}
		c.width = min(c.width, limit)
		return found
	}
	hasConfidence := fit(&conf, 22)
	fit(&cat, 14)
	fit(&rule, 28)
	fit(&obj, 30)
	fit(&vc, 16)
	// Two objects of the same name on two vCenters read as one finding
	// listed twice without it, so it is drawn whenever findings span more
	// than one vCenter.
	contexts := map[string]bool{}
	for _, f := range findings {
		contexts[f.Object.Context] = true
	}
	hasVCenter := len(contexts) > 1
	// withVCenter puts VCENTER before OBJECT, which it qualifies.
	withVCenter := func(cols ...healthColumn) []healthColumn {
		if !hasVCenter {
			return cols
		}
		out := make([]healthColumn, 0, len(cols)+1)
		for _, c := range cols {
			if c.title == obj.title {
				out = append(out, vc)
			}
			out = append(out, c)
		}
		return out
	}

	// fits reports whether the columns, their gaps, the two
	// column indent and the minimum message width all fit.
	fits := func(cols []healthColumn) bool {
		used := 2 + healthMessageMin
		for _, c := range cols {
			used += c.width + cellGap
		}
		return used <= width
	}
	cols := []healthColumn{sev}
	if hasConfidence {
		cols = append(cols, conf)
	}
	cols = withVCenter(append(cols, cat, rule, obj)...)
	if !fits(cols) && hasConfidence {
		cols = withVCenter(sev, cat, rule, obj)
	}
	if !fits(cols) {
		cols = withVCenter(sev, rule, obj)
	}
	if !fits(cols) {
		// The detail names the vCenter of a narrow terminal's findings.
		cols = []healthColumn{sev, rule, obj}
	}
	if !fits(cols) {
		// Still too wide: hold the message at a smaller floor, cap RULE (its
		// ids are the most compressible text here), and give OBJECT what the
		// rest leaves.
		rule.width = min(rule.width, 22)
		obj.width = max(12, min(obj.width, width-2-(sev.width+cellGap)-(rule.width+cellGap)-cellGap-healthMessageSqueezed))
		cols = []healthColumn{sev, rule, obj}
	}
	used := 2
	for _, c := range cols {
		used += c.width + cellGap
	}
	msg.width = max(1, width-used)
	return append(cols, msg)
}
