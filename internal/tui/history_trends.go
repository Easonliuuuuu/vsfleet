package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/humanize"
)

// trendAttentionDays is how close a datastore's projected fill date must be
// before Trends lists it under NEEDS ATTENTION.
const trendAttentionDays = 30

func (m *Model) viewHistoryTrends() []string {
	t := m.theme
	// Scrolling is advertised by the shared footer, alongside the rest of the
	// History pane bindings. Keep this subtitle about the data being shown.
	sub := "complete assessments only"
	if m.historyChurn != nil && len(m.historyChurn.Points) > 0 && m.historySnapshots != nil {
		sub = trendWindowLabel(m.historyChurn.Points)
	}
	if scope := m.historyScopeLabel(); scope != "" {
		sub = scope + " · " + sub
	}
	head := []string{t.title.Render("Trends"), "", t.dim.Render("  " + sub)}
	if len(m.historyTrendsEmpty) > 0 {
		lines := append(head, "")
		explanation := append([]string(nil), m.historyTrendsEmpty...)
		action := "Fix collection failures, then capture a complete assessment."
		if m.canCapture() {
			action = "Fix collection failures, then press n to capture again."
		}
		if m.demo {
			action = "Demo history is fixed; complete runs are needed for Trends."
		}
		explanation = append(explanation, action)
		for _, paragraph := range explanation {
			for _, line := range wrap(paragraph, m.width-4) {
				lines = append(lines, "  "+t.warn.Render(line))
			}
		}
		m.offset = clamp(m.offset, 0, max(0, len(lines)-m.bodyHeight()))
		return scrollLines(lines, m.offset, m.bodyHeight())
	}
	if m.historyTrendsErr != nil {
		return append(head, t.warn.Render("  "+m.historyTrendsErr.Error()))
	}
	if m.historyChurn == nil || m.historySnapshots == nil {
		return append(head, t.dim.Render("  loading history trends…"))
	}
	if len(m.historyChurn.Points) == 0 {
		return append(head, t.dim.Render("  no complete assessments stored — capture a complete run"))
	}
	lines := append(head, "")
	lines = append(lines, m.trendAttentionLines()...)
	var sections []trendSection
	for _, s := range []*trendSection{m.trendCapacitySection(), m.trendDatastoreSection(), m.trendEstateSection()} {
		if s != nil {
			sections = append(sections, *s)
		}
	}
	lines = append(lines, m.renderTrendSections(sections)...)
	h := m.bodyHeight()
	m.offset = clamp(m.offset, 0, max(0, len(lines)-h))
	return scrollLines(lines, m.offset, h)
}

// trendWindowLabel names the runs the pane is drawn from, so the subtitle
// says what was used rather than what was asked for.
func trendWindowLabel(points []assessment.ChurnPoint) string {
	first, last := points[0].Run.StartedAt.Local(), points[len(points)-1].Run.StartedAt.Local()
	if len(points) == 1 {
		return "1 complete run · " + first.Format("2 Jan 2006")
	}
	return fmt.Sprintf("%d complete runs · %s → %s (%s)", len(points), first.Format("2 Jan"), last.Format("2 Jan"), trendSpan(last.Sub(first)))
}

func trendSpan(d time.Duration) string {
	days := int(math.Round(d.Hours() / 24))
	switch {
	case days < 1:
		return "under a day"
	case days == 1:
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

// trendAttentionLines leads the pane with what an operator should act on:
// datastores projected to fill soon, and stale snapshots that are piling up.
// Each item's evidence sits at the right edge. When nothing qualifies it says
// so, so a quiet estate reads as checked.
func (m *Model) trendAttentionLines() []string {
	t := m.theme
	type item struct{ lead, detail string }
	var items []item
	if r := m.historyCapacityReport; r != nil {
		var soon []assessment.DatastoreCapacity
		for _, ds := range r.Datastores {
			if days := projectedDays(ds); days != nil && *days <= trendAttentionDays {
				soon = append(soon, ds)
			}
		}
		sort.SliceStable(soon, func(i, j int) bool { return *projectedDays(soon[i]) < *projectedDays(soon[j]) })
		for _, ds := range soon {
			days := *projectedDays(ds)
			style := runwayStyle(t, days)
			lead := style.Render("●") + " " + middleTruncate(ds.Object.Name, clamp(m.width-30, 12, 40)) + " fills in " + style.Render(fillsIn(days))
			var detail []string
			if ds.UsedGrowthBytes != nil && ds.SpanDays > 0 {
				detail = append(detail, signedBytes(*ds.UsedGrowthBytes)+" in "+trendSpan(time.Duration(ds.SpanDays*24*float64(time.Hour))))
			}
			if ds.Projection.Confidence == assessment.ProjectionLow {
				detail = append(detail, "low confidence")
			}
			items = append(items, item{lead, strings.Join(detail, " · ")})
		}
	}
	if points := m.historySnapshots.Points; len(points) > 1 {
		first, last := points[0].Stale, points[len(points)-1].Stale
		if last > first {
			items = append(items, item{t.warn.Render("▲") + fmt.Sprintf(" stale snapshots rose from %d to %d", first, last), "older than 30 days"})
		}
	}
	out := []string{t.header.Render("  NEEDS ATTENTION")}
	if len(items) == 0 {
		msg := fmt.Sprintf("No datastore fills within %d days, and stale snapshots are not rising.", trendAttentionDays)
		for i, line := range wrap(msg, m.width-6) {
			prefix := "  " + t.ok.Render("✓") + " "
			if i > 0 {
				prefix = "    "
			}
			out = append(out, prefix+line)
		}
		return append(out, "")
	}
	for _, it := range items {
		line := "  " + it.lead
		if it.detail != "" {
			gap := m.width - 2 - ansi.StringWidth(line) - ansi.StringWidth(it.detail)
			if gap < 3 {
				out = append(out, line)
				line, gap = "    ", m.width-6-ansi.StringWidth(it.detail)
			}
			line += strings.Repeat(" ", max(0, gap)) + t.dim.Render(it.detail)
		}
		out = append(out, line)
	}
	return append(out, "")
}

// trendSection is one block of the shared Trends grid: a label column, a
// chart column that takes the remaining width, and three number columns
// pinned to the right edge so every section's numbers line up.
type trendSection struct {
	title     string
	heads     [3]string
	chartHead func(w int) string
	rows      []trendRow
	footer    string
}

type trendRow struct {
	label  string
	chart  func(w int) string
	cells  [3]string
	styles [3]*lipgloss.Style
}

// trendGrid is the column layout shared by every section. Below the width a
// readable label and chart need beside the numbers, rows stack: label and
// numbers on one line, the chart full width beneath.
type trendGrid struct {
	labelW, chartW int
	cellW          [3]int
	stacked        bool
}

func (m *Model) trendGridFor(sections []trendSection) trendGrid {
	g := trendGrid{}
	labelW := 0
	for _, s := range sections {
		for i, h := range s.heads {
			g.cellW[i] = max(g.cellW[i], ansi.StringWidth(h))
		}
		for _, r := range s.rows {
			labelW = max(labelW, ansi.StringWidth(r.label))
			for i, c := range r.cells {
				g.cellW[i] = max(g.cellW[i], ansi.StringWidth(c))
			}
		}
	}
	inner := m.width - 4
	right := g.cellW[0] + 2 + g.cellW[1] + 2 + g.cellW[2]
	g.labelW = clamp(labelW, 15, 24)
	if spare := inner - g.labelW - 2 - right - 2; spare < 16 {
		g.labelW = max(15, g.labelW-(16-spare))
	}
	g.chartW = inner - g.labelW - 2 - right - 2
	if g.chartW < 12 {
		g.stacked = true
		g.labelW = max(0, inner-right-2)
		g.chartW = max(1, inner)
	}
	return g
}

// renderTrendSections lays every section on one grid. Rows within a section
// are separated by a blank line so full-height bars never fuse into a block.
func (m *Model) renderTrendSections(sections []trendSection) []string {
	t := m.theme
	g := m.trendGridFor(sections)
	cells := func(c [3]string, styles [3]*lipgloss.Style) string {
		var b strings.Builder
		for i, s := range c {
			if i > 0 {
				b.WriteString("  ")
			}
			text := fmt.Sprintf("%*s", g.cellW[i], s)
			if styles[i] != nil {
				text = styles[i].Render(text)
			}
			b.WriteString(text)
		}
		return b.String()
	}
	var out []string
	for _, s := range sections {
		head := fmt.Sprintf("  %-*s  ", g.labelW, s.title)
		if !g.stacked {
			chart := ""
			if s.chartHead != nil {
				chart = s.chartHead(g.chartW)
			}
			head += fmt.Sprintf("%-*s  ", g.chartW, chart)
		}
		header := &t.header
		out = append(out, t.header.Render(head)+cells(s.heads, [3]*lipgloss.Style{header, header, header}))
		for i, r := range s.rows {
			if i > 0 {
				out = append(out, "")
			}
			label := fmt.Sprintf("  %-*s  ", g.labelW, middleTruncate(r.label, g.labelW))
			if g.stacked {
				out = append(out, label+cells(r.cells, r.styles), "  "+r.chart(g.chartW))
				continue
			}
			out = append(out, label+r.chart(g.chartW)+"  "+cells(r.cells, r.styles))
		}
		if s.footer != "" {
			out = append(out, t.dim.Render("  "+s.footer))
		}
		out = append(out, "")
	}
	return out
}

// trendCapacitySection is the estate's latest CPU, memory and storage, each
// as used of total with how far it moved across the window. CPU and memory
// come from hosts, falling back to clusters; storage comes from datastores.
func (m *Model) trendCapacitySection() *trendSection {
	t := m.theme
	if m.historyCapacity == nil {
		return nil
	}
	estate := map[string][]assessment.CapacityPoint{}
	for _, series := range m.historyCapacity.Series {
		if series.Scope == "estate" && len(series.Points) > 0 {
			estate[series.Kind] = series.Points
		}
	}
	compute := estate["host"]
	if len(compute) == 0 {
		compute = estate["cluster"]
	}
	s := &trendSection{title: "CAPACITY NOW", heads: [3]string{"USED", "USED / TOTAL", ""}}
	add := func(label string, pct *float64, amount, change string) {
		r := trendRow{label: label, chart: m.trendBar(pct, &t.accent), cells: [3]string{percentCell(pct), amount, change}}
		r.styles[2] = &t.dim
		if change != "" {
			s.heads[2] = "CHANGE"
		}
		s.rows = append(s.rows, r)
	}
	pointsChange := func(first, last *float64) string {
		if first == nil || last == nil {
			return ""
		}
		return fmt.Sprintf("%+.0f pts", *last-*first)
	}
	if n := len(compute); n > 0 {
		first, last := compute[0], compute[n-1]
		if last.CPUCapacity != nil {
			amount, change := humanize.MHz(int64(*last.CPUCapacity))+" total", ""
			if last.CPUUsed != nil && last.CPUUtilization != nil {
				amount = humanize.MHz(int64(*last.CPUUsed)) + " / " + humanize.MHz(int64(*last.CPUCapacity))
			}
			if n > 1 {
				change = pointsChange(first.CPUUtilization, last.CPUUtilization)
			}
			add("CPU", last.CPUUtilization, amount, change)
		}
		if last.MemoryCapacity != nil {
			amount, change := humanize.MBPrecise(int64(*last.MemoryCapacity))+" total", ""
			if last.MemoryUsed != nil && last.MemoryUtilization != nil {
				amount = humanize.MBPrecise(int64(*last.MemoryUsed)) + " / " + humanize.MBPrecise(int64(*last.MemoryCapacity))
			}
			if n > 1 {
				change = pointsChange(first.MemoryUtilization, last.MemoryUtilization)
			}
			add("Memory", last.MemoryUtilization, amount, change)
		}
	}
	if storage := estate["datastore"]; len(storage) > 0 {
		first, last := storage[0], storage[len(storage)-1]
		if last.StorageCapacity != nil {
			amount, change := tuiBytes(*last.StorageCapacity)+" total", ""
			if last.StorageUsed != nil && last.StorageUtilization != nil {
				amount = tuiBytes(*last.StorageUsed) + " / " + tuiBytes(*last.StorageCapacity)
			}
			if len(storage) > 1 && first.StorageUsed != nil && last.StorageUsed != nil {
				change = signedBytes(*last.StorageUsed - *first.StorageUsed)
			}
			add("Storage", last.StorageUtilization, amount, change)
		}
	}
	if len(s.rows) == 0 {
		return nil
	}
	return s
}

// trendDatastoreSection ranks datastores by how soon they fill, then by
// growth, with how full each is now. Datastores with neither growth nor a
// projection fold into one line: a wall of empty rows buries the ones that
// matter.
func (m *Model) trendDatastoreSection() *trendSection {
	t := m.theme
	if m.historyCapacityReport == nil || len(m.historyCapacityReport.Datastores) == 0 {
		return nil
	}
	var known, unknown []assessment.DatastoreCapacity
	for _, ds := range m.historyCapacityReport.Datastores {
		if ds.UsedGrowthBytes == nil && projectedDays(ds) == nil {
			unknown = append(unknown, ds)
			continue
		}
		known = append(known, ds)
	}
	sort.SliceStable(known, func(i, j int) bool {
		di, dj := projectedDays(known[i]), projectedDays(known[j])
		switch {
		case di != nil && dj != nil:
			return *di < *dj
		case (di != nil) != (dj != nil):
			return di != nil
		}
		gi, gj := known[i].UsedGrowthBytes, known[j].UsedGrowthBytes
		return gi != nil && gj != nil && *gi > *gj
	})
	s := &trendSection{title: "DATASTORE", heads: [3]string{"USED", "GROWTH/WEEK", "FILLS IN"}}
	for _, ds := range known {
		style := &t.accent
		days := projectedDays(ds)
		if days != nil {
			rs := runwayStyle(t, *days)
			style = &rs
		}
		var pct *float64
		if p, ok := datastoreUsedPercent(ds); ok {
			pct = &p
		}
		r := trendRow{label: ds.Object.Name, chart: m.trendBar(pct, style), cells: [3]string{percentCell(pct), "—", "—"}}
		r.styles = [3]*lipgloss.Style{nil, &t.dim, &t.dim}
		if ds.UsedGrowthBytes != nil && ds.SpanDays >= 1 {
			r.cells[1], r.styles[1] = signedBytes(*ds.UsedGrowthBytes/ds.SpanDays*7), nil
		}
		switch {
		case days != nil:
			r.cells[2], r.styles[2] = fillsIn(*days), &t.text
			if *days <= trendAttentionDays {
				r.styles[2] = style
			}
		case ds.UsedGrowthBytes != nil && *ds.UsedGrowthBytes < 0:
			r.cells[2] = "shrinking"
		}
		s.rows = append(s.rows, r)
	}
	if len(unknown) > 0 {
		noun := "datastores"
		if len(unknown) == 1 {
			noun = "datastore"
		}
		s.footer = fmt.Sprintf("+ %d %s without enough history to show growth", len(unknown), noun)
	}
	return s
}

// trendEstateSection draws how the estate moved across the window, one
// column per run. Each chart spans its own lowest to highest value to show
// the shape, so the first and last values and the change sit beside it.
func (m *Model) trendEstateSection() *trendSection {
	t := m.theme
	points := m.historyChurn.Points
	var vms, changes, snaps, stale []float64
	for _, p := range points {
		vms = append(vms, float64(p.VMCount))
		changes = append(changes, float64(p.Appeared+p.Vanished+p.Moved+p.Modified))
	}
	for _, p := range m.historySnapshots.Points {
		snaps = append(snaps, float64(p.Total))
		stale = append(stale, float64(p.Stale))
	}
	s := &trendSection{title: "ESTATE OVER TIME", heads: [3]string{"", "FIRST → LAST", "CHANGE"}}
	if len(points) > 1 {
		from, to := points[0].Run.StartedAt.Local().Format("2 Jan"), points[len(points)-1].Run.StartedAt.Local().Format("2 Jan")
		s.chartHead = func(w int) string {
			used := ansi.StringWidth(trendColumns(vms, w))
			if len(from)+1+len(to) > used {
				return ""
			}
			return from + strings.Repeat(" ", used-len(from)-len(to)) + to
		}
	}
	add := func(label string, values []float64, style *lipgloss.Style, span, change string) {
		if len(values) == 0 {
			return
		}
		r := trendRow{label: label, cells: [3]string{"", span, change}}
		r.chart = func(w int) string { return style.Render(fmt.Sprintf("%-*s", w, trendColumns(values, w))) }
		r.styles[2] = &t.dim
		s.rows = append(s.rows, r)
	}
	fromTo := func(v []float64) (string, string) {
		if len(v) == 1 {
			return fmt.Sprintf("%.0f", v[0]), ""
		}
		first, last := v[0], v[len(v)-1]
		return fmt.Sprintf("%.0f → %.0f", first, last), fmt.Sprintf("%+.0f", last-first)
	}
	span, change := fromTo(vms)
	add("VMs", vms, &t.accent, span, change)
	if len(snaps) > 0 {
		span, change = fromTo(snaps)
		add("Snapshots", snaps, &t.accent, span, change)
		staleStyle := &t.accent
		if len(stale) > 1 && stale[len(stale)-1] > stale[0] {
			staleStyle = &t.warn
		}
		span, change = fromTo(stale)
		add("Stale snapshots", stale, staleStyle, span, change)
	}
	total := 0.0
	for _, c := range changes {
		total += c
	}
	add("Changes per run", changes, &t.accent, fmt.Sprintf("avg %.1f", total/float64(len(changes))), "")
	return s
}

// trendBar is a used-percentage bar the full chart width: solid blocks for
// the used share on a faint dotted track.
func (m *Model) trendBar(pct *float64, style *lipgloss.Style) func(w int) string {
	return func(w int) string {
		if pct == nil {
			return m.theme.dim.Render(fmt.Sprintf("%-*s", w, "—"))
		}
		filled := clamp(int(math.Round(*pct/100*float64(w))), 0, w)
		if *pct > 0 && filled == 0 {
			filled = 1
		}
		return style.Render(strings.Repeat("█", filled)) + m.theme.faint.Render(strings.Repeat("·", w-filled))
	}
}

func percentCell(pct *float64) string {
	if pct == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", *pct)
}

// trendColumns draws one equal-width column per run within w cells, from the
// lowest to the highest value, with a one-cell gap between runs when each has
// three cells or more. With more runs than cells it samples one per cell,
// keeping the first and last so the ends match the numbers beside it.
func trendColumns(values []float64, w int) string {
	if len(values) == 0 || w <= 0 {
		return ""
	}
	glyphs := []rune("▁▂▃▄▅▆▇█")
	lo, hi := values[0], values[0]
	for _, v := range values {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	glyph := func(v float64) string {
		if hi == lo {
			return string(glyphs[0])
		}
		return string(glyphs[int(math.Round((v-lo)/(hi-lo)*float64(len(glyphs)-1)))])
	}
	var b strings.Builder
	if len(values) > w {
		for i := 0; i < w; i++ {
			v := values[0]
			if w > 1 {
				v = values[int(math.Round(float64(i)*float64(len(values)-1)/float64(w-1)))]
			}
			b.WriteString(glyph(v))
		}
		return b.String()
	}
	per := w / len(values)
	gap := 0
	if per >= 3 {
		gap = 1
	}
	for i, v := range values {
		b.WriteString(strings.Repeat(glyph(v), per-gap))
		if i < len(values)-1 {
			b.WriteString(strings.Repeat(" ", gap))
		}
	}
	return b.String()
}

func projectedDays(ds assessment.DatastoreCapacity) *float64 {
	if ds.Projection == nil {
		return nil
	}
	return ds.Projection.DaysRemaining
}

func datastoreUsedPercent(ds assessment.DatastoreCapacity) (float64, bool) {
	switch {
	case ds.FreePercent != nil:
		return 100 - *ds.FreePercent, true
	case ds.CapacityBytes != nil && ds.FreeBytes != nil && *ds.CapacityBytes > 0:
		return (*ds.CapacityBytes - *ds.FreeBytes) / *ds.CapacityBytes * 100, true
	}
	return 0, false
}

func runwayStyle(t theme, days float64) lipgloss.Style {
	switch {
	case days <= 7:
		return t.bad
	case days <= trendAttentionDays:
		return t.warn
	}
	return t.accent
}

func fillsIn(days float64) string {
	switch d := math.Round(days); {
	case days < 1:
		return "<1 day"
	case d == 1:
		return "~1 day"
	default:
		return fmt.Sprintf("~%.0f days", d)
	}
}

func signedBytes(v float64) string {
	if v > 0 {
		return "+" + tuiBytes(v)
	}
	return tuiBytes(v)
}

// middleTruncate shortens a long identifier from the middle, keeping both the
// distinguishing prefix and suffix that datastore names tend to differ in.
func middleTruncate(s string, w int) string {
	r := []rune(s)
	if len(r) <= w || w < 5 {
		return s
	}
	keep := w - 1
	head := keep / 2
	return string(r[:head]) + "…" + string(r[len(r)-(keep-head):])
}

func tuiBytes(value float64) string {
	if value < 0 {
		return "-" + humanize.Bytes(int64(-value))
	}
	return humanize.Bytes(int64(value))
}
