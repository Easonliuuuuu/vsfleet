package tui

// guideEntry is one term a History pane draws without explaining, and what
// it means.
type guideEntry struct {
	term string
	text string
}

// guideTermWidth is the column the explanations start in, wide enough for
// the longest term ("CONFIDENCE") plus a gap.
const guideTermWidth = 11

// historyGuide is the "Reading this pane" block "?" shows beside a History
// pane's keys. The panes keep their screen rows for data, so the words, marks
// and columns they use without a caption are explained here instead. Trends
// has none: its empty states already say why there is nothing to show.
//
// The block sits under the hub keys in the help panel's right column, which
// has about 30 columns for an explanation at 80 wide and must not push the
// panel past a 24 row terminal, so each one is a phrase rather than a
// sentence; the full account is in docs/tui.md.
func historyGuide(pane int) []guideEntry {
	switch pane {
	case historyPaneRuns:
		return []guideEntry{
			{"complete", "every vCenter; feeds Trends"},
			{"partial", "some vCenters not reached"},
			{"failed", "capture did not finish"},
			{"📌", "pinned: kept by cleanup"},
		}
	case historyPaneHealth:
		return []guideEntry{
			{"BLOCKED", "a migration warning or worse"},
			{"UNKNOWN", "migration rules unjudged"},
			{"READY", "no blockers, all judged"},
			{"CONFIDENCE", "orphaned disks only"},
			{"thresholds", "tune: vsfleet analyze health"},
		}
	case historyPaneChanges:
		return []guideEntry{
			{"b / t", "the two runs compared"},
			{"VMs", "VM count; · none stored"},
			{"partial", "not every vCenter reached"},
			{"infra", "non-VM objects changed"},
			{"1 " + impactBlocks.String(), "fix before cutover"},
			{"2 " + impactSizing.String(), "CPU, memory, disk, capacity"},
			{"3 " + impactGrowth.String(), "new objects"},
			{"4 " + impactChurn.String(), "renames, moves, snapshots"},
		}
	}
	return nil
}

// historyGuideLines renders the guide for a pane at width, wrapping each
// explanation under its own column so a narrow terminal keeps the terms
// readable down the left edge.
func (m *Model) historyGuideLines(pane, width int) []string {
	entries := historyGuide(pane)
	if len(entries) == 0 {
		return nil
	}
	t := m.theme
	lines := []string{t.header.Render("Reading this pane")}
	indent := pad("", 2+guideTermWidth, false)
	for _, e := range entries {
		for i, l := range wrap(e.text, max(12, width-2-guideTermWidth)) {
			lead := indent
			if i == 0 {
				lead = "  " + t.accent.Render(pad(e.term, guideTermWidth, false))
			}
			lines = append(lines, lead+t.dim.Render(l))
		}
	}
	return lines
}
