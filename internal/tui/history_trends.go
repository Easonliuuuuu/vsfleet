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
	lines = append(lines, m.trendCapacityLines()...)
	lines = append(lines, m.trendDatastoreLines()...)
	lines = append(lines, m.trendEstateLines()...)
	h := m.bodyHeight()
	m.offset = clamp(m.offset, 0, max(0, len(lines)-h))
	return scrollLines(lines, m.offset, h)
}

// trendWindowLabel names the runs the pane is drawn from, so the subtitle
// says what was used rather than what was asked for.
func trendWindowLabel(points []assessment.ChurnPoint) string {
	first, last := points[0].Run.StartedAt.Local(), points[len(points)-1].Run.StartedAt.Local()
	runs := "1 complete run"
	if len(points) > 1 {
		runs = fmt.Sprintf("%d complete runs", len(points))
	}
	if len(points) == 1 {
		return runs + " · " + first.Format("2 Jan 2006")
	}
	return fmt.Sprintf("%s · %s → %s (%s)", runs, first.Format("2 Jan"), last.Format("2 Jan"), trendSpan(last.Sub(first)))
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
// When nothing qualifies it says so, so a quiet estate reads as checked.
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
	leadW := 0
	for _, it := range items {
		leadW = max(leadW, ansi.StringWidth(it.lead))
	}
	for _, it := range items {
		line := "  " + it.lead
		switch {
		case it.detail == "":
		case 2+leadW+3+ansi.StringWidth(it.detail) <= m.width:
			line += strings.Repeat(" ", leadW-ansi.StringWidth(it.lead)+3) + t.dim.Render(it.detail)
		default:
			out = append(out, line)
			line = "    " + t.dim.Render(it.detail)
		}
		out = append(out, line)
	}
	return append(out, "")
}

// trendCapacityLines is the estate's latest CPU, memory and storage, each as
// used of total with how far it moved across the window. CPU and memory come
// from hosts, falling back to clusters; storage comes from datastores.
func (m *Model) trendCapacityLines() []string {
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
	type capRow struct {
		label, amount, change string
		pct                   *float64
	}
	var rows []capRow
	pointsChange := func(first, last *float64) string {
		if first == nil || last == nil {
			return ""
		}
		return fmt.Sprintf("%+.0f pts", *last-*first)
	}
	if n := len(compute); n > 0 {
		first, last := compute[0], compute[n-1]
		if last.CPUCapacity != nil {
			r := capRow{label: "CPU", amount: humanize.MHz(int64(*last.CPUCapacity)) + " total"}
			if last.CPUUsed != nil && last.CPUUtilization != nil {
				r.amount = humanize.MHz(int64(*last.CPUUsed)) + " / " + humanize.MHz(int64(*last.CPUCapacity))
				r.pct = last.CPUUtilization
			}
			if n > 1 {
				r.change = pointsChange(first.CPUUtilization, last.CPUUtilization)
			}
			rows = append(rows, r)
		}
		if last.MemoryCapacity != nil {
			r := capRow{label: "Memory", amount: humanize.MBPrecise(int64(*last.MemoryCapacity)) + " total"}
			if last.MemoryUsed != nil && last.MemoryUtilization != nil {
				r.amount = humanize.MBPrecise(int64(*last.MemoryUsed)) + " / " + humanize.MBPrecise(int64(*last.MemoryCapacity))
				r.pct = last.MemoryUtilization
			}
			if n > 1 {
				r.change = pointsChange(first.MemoryUtilization, last.MemoryUtilization)
			}
			rows = append(rows, r)
		}
	}
	if storage := estate["datastore"]; len(storage) > 0 {
		first, last := storage[0], storage[len(storage)-1]
		if last.StorageCapacity != nil {
			r := capRow{label: "Storage", amount: tuiBytes(*last.StorageCapacity) + " total"}
			if last.StorageUsed != nil && last.StorageUtilization != nil {
				r.amount = tuiBytes(*last.StorageUsed) + " / " + tuiBytes(*last.StorageCapacity)
				r.pct = last.StorageUtilization
			}
			if len(storage) > 1 && first.StorageUsed != nil && last.StorageUsed != nil {
				r.change = signedBytes(*last.StorageUsed - *first.StorageUsed)
			}
			rows = append(rows, r)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	barW := clamp(m.width-52, 8, 24)
	amountW := 0
	for _, r := range rows {
		amountW = max(amountW, len(r.amount))
	}
	change := ""
	for _, r := range rows {
		if r.change != "" {
			change = "CHANGE"
		}
	}
	out := []string{t.header.Render(fmt.Sprintf("  %-*s  %-*s  %s", 9+barW+5, "CAPACITY NOW", amountW, "USED / TOTAL", change))}
	for _, r := range rows {
		bar := t.dim.Render(fmt.Sprintf("%-*s", barW+5, "—"))
		if r.pct != nil {
			bar = t.accent.Render(fmt.Sprintf("%-*s", barW+5, usageBar(*r.pct, barW)))
		}
		out = append(out, fmt.Sprintf("  %-7s  %s  %-*s  %s", r.label, bar, amountW, r.amount, t.dim.Render(r.change)))
	}
	return append(out, "")
}

// trendDatastoreLines ranks datastores by how soon they fill, then by growth,
// with how full each is now. Datastores with neither growth nor a projection
// fold into one line: a wall of empty rows buries the ones that matter.
func (m *Model) trendDatastoreLines() []string {
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
	barW := clamp(m.width-62, 6, 20)
	nameW := clamp(m.width-4-(barW+5)-2-11-2-10, 10, 32)
	out := []string{t.header.Render(fmt.Sprintf("  %-*s  %-*s  %11s  %10s", nameW, "DATASTORE", barW+5, "USED", "GROWTH/WEEK", "FILLS IN"))}
	for _, ds := range known {
		style := t.accent
		days := projectedDays(ds)
		if days != nil {
			style = runwayStyle(t, *days)
		}
		used := t.dim.Render(fmt.Sprintf("%-*s", barW+5, "—"))
		if pct, ok := datastoreUsedPercent(ds); ok {
			used = style.Render(fmt.Sprintf("%-*s", barW+5, usageBar(pct, barW)))
		}
		growth := t.dim.Render(fmt.Sprintf("%11s", "—"))
		if ds.UsedGrowthBytes != nil && ds.SpanDays >= 1 {
			growth = fmt.Sprintf("%11s", signedBytes(*ds.UsedGrowthBytes/ds.SpanDays*7))
		}
		runway := t.dim.Render(fmt.Sprintf("%10s", "—"))
		switch {
		case days != nil:
			s := t.text
			if *days <= trendAttentionDays {
				s = style
			}
			runway = s.Render(fmt.Sprintf("%10s", fillsIn(*days)))
		case ds.UsedGrowthBytes != nil && *ds.UsedGrowthBytes < 0:
			runway = t.dim.Render(fmt.Sprintf("%10s", "shrinking"))
		}
		out = append(out, fmt.Sprintf("  %-*s  %s  %s  %s", nameW, middleTruncate(ds.Object.Name, nameW), used, growth, runway))
	}
	if len(unknown) > 0 {
		noun := "datastores"
		if len(unknown) == 1 {
			noun = "datastore"
		}
		out = append(out, t.dim.Render(fmt.Sprintf("  + %d %s without enough history to show growth", len(unknown), noun)))
	}
	return append(out, "")
}

// trendEstateLines draws how the estate moved across the window. Each
// sparkline spans its own lowest to highest value to show the shape, so the
// first and last values and the change are printed beside it for scale.
func (m *Model) trendEstateLines() []string {
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
	const labelW = 16
	sparkW := clamp(m.width-2-labelW-2-2-18, 10, 30)
	header := fmt.Sprintf("  %-*s  ", labelW, "ESTATE OVER TIME")
	if from, to := points[0].Run.StartedAt.Local().Format("2 Jan"), points[len(points)-1].Run.StartedAt.Local().Format("2 Jan"); len(points) > 1 && len(from)+1+len(to) <= sparkW {
		header += from + strings.Repeat(" ", sparkW-len(from)-len(to)) + to
	}
	out := []string{t.header.Render(header)}
	fromTo := func(v []float64) string {
		first, last := v[0], v[len(v)-1]
		if len(v) == 1 {
			return fmt.Sprintf("%.0f", last)
		}
		return fmt.Sprintf("%.0f → %.0f  %+.0f", first, last, last-first)
	}
	row := func(label string, values []float64, style lipgloss.Style, summary string) {
		if len(values) == 0 {
			return
		}
		out = append(out, fmt.Sprintf("  %-*s  %s  %s", labelW, label, style.Render(trendSparkline(values, sparkW)), summary))
	}
	row("VMs", vms, t.accent, fromTo(vms))
	row("Snapshots", snaps, t.accent, fromTo(snaps))
	staleStyle := t.accent
	if len(stale) > 1 && stale[len(stale)-1] > stale[0] {
		staleStyle = t.warn
	}
	if len(stale) > 0 {
		row("Stale snapshots", stale, staleStyle, fromTo(stale))
	}
	total := 0.0
	for _, c := range changes {
		total += c
	}
	row("Changes per run", changes, t.accent, fmt.Sprintf("avg %.1f", total/float64(len(changes))))
	return out
}

// trendSparkline draws values across w cells from their lowest to highest
// value, always including the first and last so the ends match the numbers
// printed beside it.
func trendSparkline(values []float64, w int) string {
	if len(values) == 0 || w <= 0 {
		return ""
	}
	glyphs := []rune("▁▂▃▄▅▆▇█")
	lo, hi := values[0], values[0]
	for _, v := range values {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	var b strings.Builder
	for i := 0; i < w; i++ {
		v := values[0]
		if w > 1 {
			v = values[int(math.Round(float64(i)*float64(len(values)-1)/float64(w-1)))]
		}
		level := 0
		if hi > lo {
			level = int(math.Round((v - lo) / (hi - lo) * float64(len(glyphs)-1)))
		}
		b.WriteRune(glyphs[level])
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
