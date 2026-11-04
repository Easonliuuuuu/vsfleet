package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// sortMenu is the browse screen's sort picker: the current tab's orders,
// one per line, each with the letter that picks it. It opens over the table
// on "s" and closes on any choice or esc.
type sortMenu struct {
	keys   []sortKey
	cursor int
}

func (m *Model) openSortMenu() {
	keys := sortKeysFor(m.kind)
	k, _ := m.sortFor(m.kind).resolve(keys)
	menu := &sortMenu{keys: keys}
	for i, c := range keys {
		if c.id == k.id {
			menu.cursor = i
		}
	}
	m.sortMenu = menu
}

// handleSortMenuKey drives the open menu. It owns every key ahead of the
// table under it, the way the detail pane's action popup does; "q" and "?"
// are answered before it, and none of its letters is either.
func (m *Model) handleSortMenuKey(msg tea.KeyMsg) {
	menu := m.sortMenu
	switch {
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Sort):
		m.sortMenu = nil
	case key.Matches(msg, m.keys.Up):
		menu.cursor = (menu.cursor + len(menu.keys) - 1) % len(menu.keys)
	case key.Matches(msg, m.keys.Down):
		menu.cursor = (menu.cursor + 1) % len(menu.keys)
	case key.Matches(msg, m.keys.Open):
		m.chooseSort(menu.keys[menu.cursor], false)
	case key.Matches(msg, m.keys.SortReverse):
		m.chooseSort(menu.keys[menu.cursor], true)
	default:
		for _, k := range menu.keys {
			if msg.String() == k.letter {
				m.chooseSort(k, false)
				return
			}
		}
	}
}

// chooseSort applies k and closes the menu. Choosing the order the table is
// already in reverses it, as does reverse; any other starts in k's own
// direction.
func (m *Model) chooseSort(k sortKey, reverse bool) {
	m.sortMenu = nil
	cur, desc := m.sortFor(m.kind).resolve(sortKeysFor(m.kind))
	switch {
	case cur.id == k.id:
		desc = !desc
	case reverse:
		desc = !k.desc
	default:
		desc = k.desc
	}
	m.setSort(sortOrder{key: k.id, desc: desc})
}

// sortMenuBox renders the open menu as a bordered box, its cursor on the
// line enter would choose and the table's current order marked with its
// direction.
func (m *Model) sortMenuBox() string {
	t := m.theme
	menu := m.sortMenu
	cur, desc := m.sortFor(m.kind).resolve(menu.keys)
	labelWidth := 0
	for _, k := range menu.keys {
		labelWidth = max(labelWidth, ansi.StringWidth(k.label))
	}
	lines := []string{t.title.Render("Sort " + tabTitle(m.kind)), ""}
	for i, k := range menu.keys {
		mark := " "
		if k.id == cur.id {
			mark = sortArrow(desc)
		}
		text := k.letter + "  " + pad(k.label, labelWidth, false) + "  " + mark
		if i == menu.cursor {
			lines = append(lines, t.focused.Render("▸ "+text))
			continue
		}
		lines = append(lines, "  "+t.accent.Render(k.letter)+t.text.Render("  "+pad(k.label, labelWidth, false)+"  ")+t.accent.Render(mark))
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(0, 2).
		Render(strings.Join(lines, "\n"))
}

// tableHeadings joins the column headings, the one titled sorted carrying
// arrow: after a left-aligned title, before a right-aligned one so it stays
// beside the numbers. A heading as wide as its column takes the arrow from
// the gap beside it rather than losing a letter.
func tableHeadings(cols []column, widths []int, sorted, arrow string) string {
	var b strings.Builder
	gapBefore := 0
	for i, c := range cols {
		if widths[i] == 0 {
			continue
		}
		cell := pad(c.title, widths[i], c.right)
		gapAfter := cellGap
		if sorted != "" && c.title == sorted {
			title := c.title
			switch {
			case c.right && ansi.StringWidth(title) < widths[i]:
				cell = pad(arrow+title, widths[i], true)
			case c.right && gapBefore > 0:
				gapBefore--
				cell = arrow + cell
			case ansi.StringWidth(title) < widths[i]:
				cell = pad(title+arrow, widths[i], false)
			default:
				cell += arrow
				gapAfter--
			}
		}
		b.WriteString(strings.Repeat(" ", gapBefore))
		b.WriteString(cell)
		gapBefore = gapAfter
	}
	return b.String()
}
