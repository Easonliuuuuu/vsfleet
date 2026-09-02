package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/easonliuuuuu/vc-tui/internal/humanize"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

// Layout constants. The interface is built for an 80x24 terminal on a jump
// host and grows from there; nothing below assumes more.
const (
	cellGap        = 2
	glyphGutter    = 2 // status glyph plus its separating space
	minNameWidth   = 16
	sidebarMin     = 18
	sidebarMax     = 26
	chromeHeight   = 4 // header, rule, message, key line
	tableChrome    = 3 // tab bar, rule, column headings
	minTermWidth   = 40
	minTermHeight  = 10
	labelColumnPad = 20
)

// View renders the whole frame.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	if m.width < minTermWidth || m.height < minTermHeight {
		return m.theme.dim.Render(fmt.Sprintf("terminal too small: %dx%d, need at least %dx%d",
			m.width, m.height, minTermWidth, minTermHeight))
	}
	var body string
	switch m.mode {
	case modeDetail:
		body = strings.Join(m.viewDetail(), "\n")
	case modeDoctor:
		body = strings.Join(m.viewDoctor(), "\n")
	case modeHelp:
		body = strings.Join(m.viewHelp(), "\n")
	case modeForm:
		body = strings.Join(m.viewForm(), "\n")
	case modeConfirmDelete:
		body = strings.Join(m.viewConfirmDelete(), "\n")
	default:
		body = m.viewBrowse()
	}
	return strings.Join([]string{
		m.viewHeader(),
		m.theme.rule.Render(strings.Repeat("─", m.width)),
		body,
		m.viewMessage(),
		m.viewKeys(),
	}, "\n")
}

func (m *Model) bodyHeight() int { return max(1, m.height-chromeHeight) }

func (m *Model) sidebarWidth() int {
	return clamp(m.width/4, sidebarMin, sidebarMax)
}

func (m *Model) contentWidth() int {
	return max(minNameWidth, m.width-m.sidebarWidth()-cellGap)
}

// tableHeight is how many resource rows fit, after the tab bar, the headings
// and any line spent reporting a vCenter that could not be read.
func (m *Model) tableHeight() int {
	return max(1, m.bodyHeight()-tableChrome-len(m.failuresInScope()))
}

func (m *Model) viewHeader() string {
	t := m.theme
	scope := "context " + t.accent.Render(m.scopeName())
	if m.allScope {
		scope = t.accent.Render(fmt.Sprintf("all %d vCenters", len(m.states)))
	}
	connected, failed := 0, 0
	for _, st := range m.states {
		switch {
		case st.err != nil:
			failed++
		case st.inv != nil:
			connected++
		}
	}
	summary := fmt.Sprintf("%d connected", connected)
	if failed > 0 {
		summary += t.bad.Render(fmt.Sprintf(" · %d failed", failed))
	}
	if m.pendingScope() {
		summary += " · " + m.spin.View() + "loading"
	}
	left := t.title.Render("vctui") + "  " + scope + t.dim.Render("  ·  "+summary)
	return truncate(left, m.width)
}

func (m *Model) scopeName() string {
	if st := m.current(); st != nil {
		return st.cc.Name
	}
	return "none"
}

func (m *Model) viewMessage() string {
	t := m.theme
	if m.filtering || m.filter.Value() != "" {
		hint := ""
		if !m.filtering && m.filter.Value() != "" {
			hint = t.dim.Render(fmt.Sprintf("  %d match(es), esc to clear", len(m.rows())))
		}
		return truncate(t.accent.Render(m.filter.View())+hint, m.width)
	}
	if m.message == "" {
		return ""
	}
	style := t.dim
	if m.messageBad {
		style = t.bad
	}
	return truncate(style.Render(m.message), m.width)
}

func (m *Model) viewKeys() string {
	t := m.theme
	parts := make([]string, 0, 10)
	for _, b := range m.keys.footerHints(m) {
		h := b.Help()
		parts = append(parts, t.accent.Render(h.Key)+" "+t.dim.Render(h.Desc))
	}
	return truncate(strings.Join(parts, t.faint.Render("  ")), m.width)
}

// viewBrowse is the main screen: contexts on the left, one resource kind on
// the right.
func (m *Model) viewBrowse() string {
	side := m.viewSidebar()
	content := m.viewContent()
	return lipgloss.JoinHorizontal(lipgloss.Top,
		padBlock(side, m.sidebarWidth(), m.bodyHeight()),
		strings.Repeat(" ", cellGap),
		padBlock(content, m.contentWidth(), m.bodyHeight()),
	)
}

// viewSidebar lists every configured vCenter with its state. Two lines each:
// the name, and the route or the reason it is not answering. The second line
// is the point — "failed" alone sends you to the logs.
func (m *Model) viewSidebar() []string {
	t := m.theme
	w := m.sidebarWidth()
	lines := []string{t.header.Render(pad("CONTEXTS", w, false))}
	for i, st := range m.states {
		marker := " "
		if i == m.selected {
			marker = glyphCursor
		}
		head := fmt.Sprintf("%s%s %s", marker, t.statusStyle(st.rowStatus()).Render(st.glyph()), st.cc.Name)
		style := t.text
		if i == m.selected {
			style = t.selected
			if m.pane == paneContexts {
				style = t.focused
			}
		}
		lines = append(lines, style.Render(pad(head, w, false)))
		lines = append(lines, t.faint.Render(pad("   "+m.sidebarDetail(st), w, false)))
	}
	return lines
}

// sidebarDetail is the second line of a context row: what it cost, or what
// went wrong.
func (m *Model) sidebarDetail(st *contextState) string {
	switch {
	case st.loading:
		return "connecting…"
	case st.diagging:
		return "diagnosing…"
	case st.err != nil:
		return firstLine(st.err.Error())
	case st.inv != nil:
		s := m.status(st.cc.Name)
		route := shortRoute(st.cc.Transport.Describe())
		if s.Latency > 0 {
			return route + " · " + humanize.Duration(s.Latency)
		}
		return route
	default:
		return shortRoute(st.cc.Transport.Describe()) + " · not connected"
	}
}

func shortRoute(s string) string {
	if i := strings.Index(s, " ->"); i > 0 {
		return s[:i]
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// viewContent is the tab bar and the resource table.
func (m *Model) viewContent() []string {
	t := m.theme
	w := m.contentWidth()
	lines := []string{m.viewTabs(w), t.rule.Render(strings.Repeat("─", w))}

	cols := columnsFor(m.kind, m.showContext())
	widths := layoutColumns(cols, w-glyphGutter)
	head := make([]string, 0, len(cols))
	for i, c := range cols {
		if widths[i] == 0 {
			continue
		}
		head = append(head, pad(c.title, widths[i], c.right))
	}
	lines = append(lines, t.header.Render(strings.Repeat(" ", glyphGutter)+strings.Join(head, strings.Repeat(" ", cellGap))))

	rows := m.rows()
	h := m.tableHeight()
	if len(rows) == 0 {
		lines = append(lines, t.dim.Render(m.emptyMessage()))
	}
	for i := m.offset; i < len(rows) && i < m.offset+h; i++ {
		lines = append(lines, m.renderRow(rows[i], cols, widths, i == m.cursor))
	}
	// Failures are listed under the table rather than replacing it: the
	// vCenters that did answer are still the answer to the question asked.
	for _, st := range m.failuresInScope() {
		lines = append(lines, t.bad.Render(truncate(
			fmt.Sprintf("%s %s: %s", glyphFail, st.cc.Name, firstLine(st.err.Error())), w)))
	}
	return lines
}

func (m *Model) emptyMessage() string {
	switch {
	case m.pendingScope():
		return m.spin.View() + "loading inventory…"
	case m.filter.Value() != "":
		return fmt.Sprintf("no %s matching %q", tabTitle(m.kind), m.filter.Value())
	case len(m.failuresInScope()) > 0:
		return ""
	default:
		return "no " + tabTitle(m.kind)
	}
}

func (m *Model) renderRow(r row, cols []column, widths []int, selected bool) string {
	t := m.theme
	cells := make([]string, 0, len(cols))
	for i, c := range cols {
		if widths[i] == 0 || i >= len(r.cells) {
			continue
		}
		cells = append(cells, pad(r.cells[i], widths[i], c.right))
	}
	line := strings.Join(cells, strings.Repeat(" ", cellGap))
	switch {
	case selected && m.pane == paneResources:
		line = t.focused.Render(line)
	case selected:
		line = t.selected.Render(line)
	default:
		line = t.text.Render(line)
	}
	// The glyph sits in its own gutter, outside the selection highlight, so a
	// powered-off VM still reads as powered off while the cursor is on it.
	return t.statusStyle(r.status).Render(r.glyph) + " " + line
}

func (m *Model) viewTabs(w int) string {
	t := m.theme
	parts := make([]string, 0, len(vsphere.AllKinds))
	for _, k := range vsphere.AllKinds {
		label := fmt.Sprintf("%s %d", tabTitle(k), m.count(k))
		if k == m.kind {
			parts = append(parts, t.tabOn.Render(label))
		} else {
			parts = append(parts, t.tabOff.Render(label))
		}
	}
	return truncate(strings.Join(parts, t.faint.Render("   ")), w)
}

// viewDetail is one object, full width, every property the inventory carries.
func (m *Model) viewDetail() []string {
	t := m.theme
	r, ok := m.currentRow()
	if !ok {
		return []string{t.dim.Render("nothing selected")}
	}
	lines := []string{
		t.title.Render(r.name) + t.dim.Render("   "+kindLabel(m.kind)+" · "+r.context),
		"",
	}
	for _, f := range r.detail {
		lines = append(lines, "  "+t.label.Render(pad(f.label, labelColumnPad, false))+t.value.Render(f.value))
	}
	for _, n := range r.notes {
		lines = append(lines, "", t.label.Render("  "+n.label))
		for _, l := range wrap(n.value, m.width-4) {
			lines = append(lines, "  "+t.value.Render(l))
		}
	}
	return scrollLines(lines, m.detailY, m.bodyHeight())
}

// viewDoctor renders a connection diagnosis stage by stage, which is the same
// walk "vctui doctor" prints.
func (m *Model) viewDoctor() []string {
	t := m.theme
	st := m.current()
	if st == nil {
		return []string{t.dim.Render("no context selected")}
	}
	lines := []string{t.title.Render("Diagnosing " + st.cc.Name), ""}
	if st.diag == nil {
		return append(lines, "  "+m.spin.View()+t.dim.Render("walking the connection…"))
	}
	d := st.diag
	for _, f := range []field{
		{"Endpoint", d.Endpoint},
		{"Route", d.Route},
		{"TLS", d.TLS},
	} {
		lines = append(lines, "  "+t.label.Render(pad(f.label, 12, false))+t.value.Render(humanize.Dash(f.value)))
	}
	lines = append(lines, "")
	lines = append(lines, m.renderChecks(d.Checks)...)
	lines = append(lines, "")
	if d.OK() {
		lines = append(lines, "  "+t.ok.Render("Connection successful.")+t.dim.Render("  "+humanize.Duration(d.Latency)))
	} else {
		lines = append(lines, "  "+t.bad.Render("Stopped at the first failing stage."))
	}
	if d.Thumbprint != "" {
		lines = append(lines, "", "  "+t.label.Render(pad("Served thumbprint", labelColumnPad, false)))
		lines = append(lines, "  "+t.value.Render(d.Thumbprint))
	}
	return scrollLines(lines, 0, m.bodyHeight())
}

// renderChecks renders a diagnosis's stages, shared by the doctor panel and
// the form's "last test" summary so a connection reads the same way in
// either place.
func (m *Model) renderChecks(checks []vsphere.Check) []string {
	t := m.theme
	lines := make([]string, 0, len(checks))
	for _, c := range checks {
		glyph, style := glyphSkip, t.faint
		switch c.Status {
		case vsphere.CheckPass:
			glyph, style = glyphOK, t.ok
		case vsphere.CheckFail:
			glyph, style = glyphFail, t.bad
		}
		detail := c.Detail
		if c.Err != nil {
			detail = firstLine(c.Err.Error())
		}
		line := fmt.Sprintf("  %s %s  %s", style.Render(glyph), pad(c.Name, 24, false), t.dim.Render(detail))
		lines = append(lines, truncate(line, m.width))
	}
	return lines
}

// viewForm renders the add/edit context form: one row per field, a cursor
// marker on the active one, and the outcome of the last test or discovery
// below it. There is no separate "review" screen — the form is always
// showing exactly what will be saved.
func (m *Model) viewForm() []string {
	t := m.theme
	f := m.form
	if f == nil {
		return []string{t.dim.Render("no form open")}
	}
	title := "Add a vCenter"
	if f.editing {
		title = "Edit " + f.origName
	}
	lines := []string{t.title.Render(title)}
	if len(m.states) == 0 && !f.editing {
		lines = append(lines, t.dim.Render("No contexts are configured yet — this is the first one."))
	}
	lines = append(lines, "")

	const labelWidth = 30
	rows := f.rows()
	for i, r := range rows {
		marker := "  "
		if i == f.cursor {
			marker = t.accent.Render("▸ ")
		}
		switch r.kind {
		case rowButton:
			style := t.dim
			if i == f.cursor {
				style = t.focused
			}
			lines = append(lines, marker+style.Render("[ "+r.static+" ]"))
		case rowStatic:
			lines = append(lines, marker+t.label.Render(pad(r.label, labelWidth, false))+t.value.Render(r.static))
		case rowSelect:
			opts := make([]string, len(r.options))
			for oi, o := range r.options {
				if oi == *r.idx {
					opts[oi] = t.accent.Render("[" + o + "]")
				} else {
					opts[oi] = t.faint.Render(" " + o + " ")
				}
			}
			lines = append(lines, marker+t.label.Render(pad(r.label, labelWidth, false))+strings.Join(opts, " "))
		case rowToggle:
			v, style := "no", t.faint
			if *r.flag {
				v, style = "yes", t.ok
			}
			lines = append(lines, marker+t.label.Render(pad(r.label, labelWidth, false))+style.Render(v))
		default: // rowText, rowSecret
			lines = append(lines, marker+t.label.Render(pad(r.label, labelWidth, false))+r.input.View())
		}
		if r.hint != "" && i == f.cursor {
			lines = append(lines, "    "+t.faint.Render(r.hint))
		}
	}

	lines = append(lines, "")
	switch {
	case f.testing:
		lines = append(lines, "  "+m.spin.View()+t.dim.Render("testing connection…"))
	case f.discovering:
		lines = append(lines, "  "+m.spin.View()+t.dim.Render("fetching certificate…"))
	case f.saving:
		lines = append(lines, "  "+m.spin.View()+t.dim.Render("saving…"))
	}
	if f.err != "" {
		lines = append(lines, "  "+t.bad.Render(f.err))
	} else if f.note != "" {
		lines = append(lines, "  "+t.ok.Render(f.note))
	}
	if f.diag != nil {
		lines = append(lines, "", t.header.Render("Last test"))
		lines = append(lines, m.renderChecks(f.diag.Checks)...)
	}
	return scrollLines(lines, 0, m.bodyHeight())
}

// viewConfirmDelete asks once, plainly, before a context and — by default —
// its stored password are gone.
func (m *Model) viewConfirmDelete() []string {
	t := m.theme
	st := m.confirmDelete
	if st == nil {
		return []string{t.dim.Render("nothing selected")}
	}
	lines := []string{
		t.title.Render("Delete " + st.cc.Name + "?"),
		"",
		"  " + t.label.Render(pad("Endpoint", 12, false)) + t.value.Render(st.cc.Endpoint),
		"  " + t.label.Render(pad("Route", 12, false)) + t.value.Render(st.cc.Transport.Describe()),
		"",
	}
	credLine := "  " + t.label.Render(pad("Stored password", 12, false))
	if m.confirmAlsoCredential {
		credLine += t.bad.Render("deleted with it")
	} else {
		credLine += t.ok.Render("kept in the keyring")
	}
	lines = append(lines, credLine, "  "+t.faint.Render("(c to toggle)"), "")
	lines = append(lines, "  "+t.bad.Render("y")+t.dim.Render(" delete    ")+t.accent.Render("n")+t.dim.Render(" cancel"))
	return scrollLines(lines, 0, m.bodyHeight())
}

func (m *Model) viewHelp() []string {
	t := m.theme
	lines := []string{t.title.Render("Keys"), ""}
	for _, sec := range m.keys.helpSections() {
		lines = append(lines, "  "+t.header.Render(sec.title))
		for _, b := range sec.bindings {
			h := b.Help()
			lines = append(lines, "    "+t.accent.Render(pad(h.Key, 10, false))+t.dim.Render(h.Desc))
		}
		lines = append(lines, "")
	}
	lines = append(lines,
		"  "+t.dim.Render("Every view here is also a command: vctui vm list, vctui search, vctui doctor."))
	return scrollLines(lines, 0, m.bodyHeight())
}

// layoutColumns assigns a width to every column, giving the flexible one what
// is left and dropping columns from the right when even that is not enough.
// A width of zero in the result means the column is not drawn.
func layoutColumns(cols []column, total int) []int {
	widths := make([]int, len(cols))
	drawn := len(cols)
	for {
		fixed, flexIdx := 0, -1
		for i := 0; i < drawn; i++ {
			if cols[i].width == 0 {
				flexIdx = i
				continue
			}
			fixed += cols[i].width
		}
		spare := total - fixed - cellGap*(drawn-1)
		if flexIdx == -1 {
			// No flexible column: shrink by dropping from the right.
			if spare >= 0 || drawn <= 1 {
				for i := 0; i < drawn; i++ {
					widths[i] = cols[i].width
				}
				return widths
			}
			drawn--
			continue
		}
		if spare >= minNameWidth || drawn <= flexIdx+1 {
			for i := 0; i < drawn; i++ {
				widths[i] = cols[i].width
			}
			widths[flexIdx] = max(minNameWidth, spare)
			return widths
		}
		drawn--
	}
}

// pad fits a string to an exact display width, truncating with an ellipsis so
// that a long VM name never pushes the columns to its right out of alignment.
func pad(s string, w int, right bool) string {
	if w <= 0 {
		return ""
	}
	s = runewidth.Truncate(s, w, "…")
	gap := w - runewidth.StringWidth(s)
	if gap <= 0 {
		return s
	}
	if right {
		return strings.Repeat(" ", gap) + s
	}
	return s + strings.Repeat(" ", gap)
}

// truncate cuts a rendered line to the terminal width without padding it.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return runewidth.Truncate(s, w, "…")
}

// padBlock forces a block of lines to an exact width and height so that two
// columns joined side by side stay square.
func padBlock(lines []string, w, h int) string {
	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		s := ""
		if i < len(lines) {
			s = truncate(lines[i], w)
		}
		if gap := w - lipgloss.Width(s); gap > 0 {
			s += strings.Repeat(" ", gap)
		}
		out = append(out, s)
	}
	return strings.Join(out, "\n")
}

// scrollLines windows a block of lines and pads it to the available height.
func scrollLines(lines []string, offset, h int) []string {
	if offset > len(lines)-1 {
		offset = max(0, len(lines)-1)
	}
	if offset < 0 {
		offset = 0
	}
	end := min(len(lines), offset+h)
	out := append([]string{}, lines[offset:end]...)
	for len(out) < h {
		out = append(out, "")
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// wrap breaks a paragraph at word boundaries, for annotations that were
// written as prose by whoever created the VM.
func wrap(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			switch {
			case line == "":
				line = word
			case runewidth.StringWidth(line)+1+runewidth.StringWidth(word) <= w:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		out = append(out, line)
	}
	return out
}
