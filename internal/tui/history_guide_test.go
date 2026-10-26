package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
)

// TestHelpExplainsWhatEachHistoryPaneShows pins the guide under the hub
// keys: the terms each pane draws without a caption are explained in "?",
// and the panel still fits the smallest terminal it is designed for.
func TestHelpExplainsWhatEachHistoryPaneShows(t *testing.T) {
	cases := []struct {
		name string
		pane int
		want []string
	}{
		{"changes", historyPaneChanges, []string{"b / t", "VMs", "partial", "infra", "1 blocks", "2 sizing", "3 growth", "4 churn"}},
		{"runs", historyPaneRuns, []string{"complete", "partial", "failed", "pinned"}},
		{"health", historyPaneHealth, []string{"BLOCKED", "UNKNOWN", "READY", "CONFIDENCE", "vsfleet analyze health"}},
		{"trends", historyPaneTrends, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := captureCapableModel(t)
			m.width, m.height = 80, 24
			m.mode, m.historyPane = modeChanges, tc.pane
			press(t, m, "?")
			view := ansi.Strip(m.View())
			if got := strings.Contains(view, "Reading this pane"); got != (tc.want != nil) {
				t.Errorf("guide shown=%v, want %v:\n%s", got, tc.want != nil, view)
			}
			for _, want := range tc.want {
				if !strings.Contains(view, want) {
					t.Errorf("help does not explain %q:\n%s", want, view)
				}
			}
			if over := len(m.helpLines()) - m.bodyHeight(); over > 0 {
				t.Errorf("help overflows an 80x24 body by %d lines:\n%s", over, view)
			}
			for _, line := range strings.Split(view, "\n") {
				if strings.Contains(line, "…") {
					t.Errorf("help clips a line: %q", line)
				}
				if w := ansi.StringWidth(line); w > 80 {
					t.Errorf("help line is %d columns wide: %q", w, line)
				}
			}
		})
	}
}

// TestScrubberDropsTheVMRowWhenNoRunHasACount pins that a capture-size row of
// nothing but "·" is left out, and that the handle row keeps its own hint
// rather than inheriting the dropped row's note.
func TestScrubberDropsTheVMRowWhenNoRunHasACount(t *testing.T) {
	runs := []assessment.Run{
		{ID: 2, Status: assessment.RunComplete},
		{ID: 1, Status: assessment.RunComplete},
	}
	cases := []struct {
		name    string
		churn   *assessment.ChurnTrend
		wantVMs bool
	}{
		{"no counts", nil, false},
		{"counts", &assessment.ChurnTrend{Points: []assessment.ChurnPoint{{Run: runs[1], VMCount: 40}, {Run: runs[0], VMCount: 44}}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t, twoHealthy(), Options{})
			m.width = 140
			m.runs = runs
			m.baseRun, m.targetRun, m.scrubHandle = 1, 2, "t"
			m.historyChurn = tc.churn
			lines := m.viewScrubber(m.width, true)
			var plain []string
			for _, l := range lines {
				plain = append(plain, ansi.Strip(l))
			}
			hasVMs := false
			for _, l := range plain {
				if strings.HasPrefix(strings.TrimSpace(l), "VMs") {
					hasVMs = true
				}
			}
			if hasVMs != tc.wantVMs {
				t.Errorf("VMs row drawn=%v, want %v:\n%s", hasVMs, tc.wantVMs, strings.Join(plain, "\n"))
			}
			handles := plain[len(plain)-1]
			if !strings.HasPrefix(strings.TrimSpace(handles), "pick") || !strings.Contains(handles, "←/→ move target") {
				t.Errorf("handle row lost its hint: %q", handles)
			}
		})
	}
}

// TestRunsPaneHeadsItsColumns pins the column heading over the run list, and
// that an empty store shows its message without a heading over nothing.
func TestRunsPaneHeadsItsColumns(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{})
	m.width, m.height = 80, 24
	m.mode, m.historyPane = modeChanges, historyPaneRuns
	m.runs = []assessment.Run{{ID: 1, Label: "baseline", Status: assessment.RunComplete}}
	view := ansi.Strip(strings.Join(m.viewHistoryHubRuns(), "\n"))
	heading := strings.Index(view, "RUN   LABEL")
	row := strings.Index(view, "#1")
	if heading < 0 || row < 0 || heading > row {
		t.Fatalf("want a RUN/LABEL/STATUS/STARTED heading above the rows:\n%s", view)
	}
	for _, col := range []string{"STATUS", "STARTED"} {
		if !strings.Contains(view, col) {
			t.Errorf("heading is missing %s:\n%s", col, view)
		}
	}

	m.runs = nil
	if view := ansi.Strip(strings.Join(m.viewHistoryHubRuns(), "\n")); strings.Contains(view, "LABEL") {
		t.Errorf("an empty store still draws the heading:\n%s", view)
	}
}
