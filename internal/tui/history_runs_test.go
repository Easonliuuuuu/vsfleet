package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/easonliuuuuu/vsfleet/internal/assessment"
)

// runsStore is a history with one finished run, for the Runs pane's edits.
func runsStore(t *testing.T) (*assessment.Store, assessment.Run) {
	t.Helper()
	store, err := assessment.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	at := time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC)
	run, err := store.StartRun(context.Background(), "test", nil, at)
	if err != nil {
		t.Fatal(err)
	}
	if run, err = store.FinishRun(context.Background(), run.ID, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	return store, run
}

// TestPinnedRunKeepsColumnsInLine checks that a pinned run's status sits in
// the same column as an unpinned one's, in the Runs pane and the picker
// (issue #474).
func TestPinnedRunKeepsColumnsInLine(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{})
	m.width, m.height = 100, 30
	at := time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC)
	m.runs = []assessment.Run{
		{ID: 5, Label: "september-audit", Status: assessment.RunPartial, Pinned: true, StartedAt: at},
		{ID: 4, Label: "pre-migration", Status: assessment.RunPartial, StartedAt: at},
	}
	for _, tc := range []struct {
		name string
		view func() []string
	}{
		{"runs pane", m.viewHistoryHubRuns},
		{"picker", m.viewHistoryRuns},
	} {
		t.Run(tc.name, func(t *testing.T) {
			column := -1
			for _, line := range tc.view() {
				line = ansi.Strip(line)
				at := strings.Index(line, "partial")
				if at < 0 {
					continue
				}
				width := ansi.StringWidth(line[:at])
				if column >= 0 && width != column {
					t.Fatalf("status starts at column %d and %d:\n%s", column, width, ansi.Strip(strings.Join(tc.view(), "\n")))
				}
				column = width
			}
			if column < 0 {
				t.Fatalf("no run rows:\n%s", ansi.Strip(strings.Join(tc.view(), "\n")))
			}
		})
	}
}

// TestRunEditStatusNamesTheRunAndTheChange checks that a run edit reports
// the run as "#N" and what changed, not "assessment N metadata updated"
// (issue #474).
func TestRunEditStatusNamesTheRunAndTheChange(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []string
		want string
	}{
		{"pin", []string{"p"}, "pinned"},
		{"unpin", []string{"p", "p"}, "unpinned"},
		{"label", []string{"e", "nightly", "enter"}, "label set"},
		{"note cleared", []string{"N", "enter"}, "note cleared"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, run := runsStore(t)
			m := newTestModel(t, twoHealthy(), Options{Current: "prod", Assessment: &assessment.Service{Store: store}})
			m.mode, m.historyPane = modeChanges, historyPaneRuns
			m.runs = []assessment.Run{run}
			press(t, m, tc.keys...)
			if want := historyRunLabel(run.ID) + " " + tc.want; m.message != want {
				t.Fatalf("status is %q, want %q", m.message, want)
			}
		})
	}
}

// TestDemoRefusesRunEdits checks that the read-only demo refuses label, note
// and pin edits the way it refuses a capture, and that the capture refusal
// does not call the store read-only while letting edits through (issue #474).
func TestDemoRefusesRunEdits(t *testing.T) {
	for _, key := range []string{"p", "e", "N"} {
		t.Run(key, func(t *testing.T) {
			store, run := runsStore(t)
			m := newTestModel(t, twoHealthy(), Options{Current: "prod", Demo: true, Assessment: &assessment.Service{Store: store}})
			m.mode, m.historyPane = modeChanges, historyPaneRuns
			m.runs = []assessment.Run{run}
			press(t, m, key)
			if m.mode != modeChanges {
				t.Fatalf("%q opened mode %v in the demo", key, m.mode)
			}
			if !strings.Contains(m.message, demoReadOnlyReason) {
				t.Fatalf("%q status is %q, want it to say the demo is read-only", key, m.message)
			}
			got, err := store.GetRun(context.Background(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Pinned != run.Pinned || got.Label != run.Label || got.Note != run.Note {
				t.Fatalf("%q changed the run in the demo: %+v", key, got)
			}
			for _, b := range defaultKeys().footerHints(m) {
				if b.Help().Key == key {
					t.Fatalf("demo footer still offers %q", key)
				}
			}
		})
	}
	t.Run("capture outside the demo", func(t *testing.T) {
		store, run := runsStore(t)
		m := newTestModel(t, twoHealthy(), Options{Current: "prod", Assessment: &assessment.Service{Store: store}})
		m.mode, m.historyPane = modeChanges, historyPaneChanges
		m.runs = []assessment.Run{run}
		press(t, m, "n")
		if strings.Contains(m.message, "read-only") {
			t.Fatalf("capture refusal calls an editable store read-only: %q", m.message)
		}
	})
}

// TestRunsPaneDetailsTheSelectedRun checks that the Runs pane says which
// vCenter made the selected run partial, and shows its note and pin, beside
// the list and under it (issue #478).
func TestRunsPaneDetailsTheSelectedRun(t *testing.T) {
	for _, width := range []int{140, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := partialEstateModel(t, Options{AllContexts: true}, width, 30)
			m.historyPane = historyPaneRuns
			m.runs[1].Note = "after the edge cutover"
			m.runs[1].Pinned = true
			press(t, m, "down")
			view := viewText(m)
			for _, want := range []string{"#2", "dr", "not reached", "edge-vc", "reached", "after the edge cutover", "PINNED", "yes"} {
				if !strings.Contains(view, want) {
					t.Fatalf("run detail lacks %q:\n%s", want, view)
				}
			}
		})
	}
}

// TestRunsPaneComparesTheSelectedRun checks that Enter opens the changes in
// the selected run since the run before it, and that b and t make it one end
// of the comparison (issue #478).
func TestRunsPaneComparesTheSelectedRun(t *testing.T) {
	for _, tc := range []struct {
		name         string
		cursor       int
		key          string
		base, target int // indices into the newest-first run list
		pane         int
	}{
		{"enter compares with the run before", 1, "enter", 2, 1, historyPaneChanges},
		{"enter on the oldest run stays", 2, "enter", 1, 0, historyPaneRuns},
		{"b sets the baseline", 2, "b", 2, 0, historyPaneChanges},
		{"t on the baseline is refused", 1, "t", 1, 0, historyPaneRuns},
		{"t past the baseline swaps the pair", 2, "t", 2, 1, historyPaneChanges},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := partialEstateModel(t, Options{AllContexts: true}, 140, 30)
			m.historyPane, m.runCursor = historyPaneRuns, tc.cursor
			press(t, m, tc.key)
			if m.baseRun != m.runs[tc.base].ID || m.targetRun != m.runs[tc.target].ID || m.historyPane != tc.pane {
				t.Fatalf("compare is %s → %s in pane %d, want %s → %s in pane %d (status %q)",
					historyRunLabel(m.baseRun), historyRunLabel(m.targetRun), m.historyPane,
					historyRunLabel(m.runs[tc.base].ID), historyRunLabel(m.runs[tc.target].ID), tc.pane, m.message)
			}
		})
	}
}

// TestRunsPaneKeepsTheCursorOnScreen checks that the run list scrolls with
// the cursor when there are more runs than rows (issue #478).
func TestRunsPaneKeepsTheCursorOnScreen(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{})
	m.width, m.height = 80, 24
	m.mode, m.historyPane = modeChanges, historyPaneRuns
	at := time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC)
	for i := 40; i > 0; i-- {
		m.runs = append(m.runs, assessment.Run{ID: int64(i), Status: assessment.RunComplete, StartedAt: at})
	}
	press(t, m, "end")
	if view := viewText(m); !strings.Contains(view, "#1 ") {
		t.Fatalf("the cursor on #1 is off screen:\n%s", view)
	}
}

// TestRunPickerLabelsItsColumnsAndKeys checks that the run picker heads its
// columns, the source among them, and offers only the keys it accepts
// (issue #478).
func TestRunPickerLabelsItsColumnsAndKeys(t *testing.T) {
	m := partialEstateModel(t, Options{AllContexts: true}, 100, 30)
	press(t, m, "R")
	if m.mode != modeHistoryRuns {
		t.Fatalf("R opened mode %v", m.mode)
	}
	if view := viewText(m); !strings.Contains(view, "SOURCE") {
		t.Fatalf("picker does not label its source column:\n%s", view)
	}
	for _, b := range m.keys.footerHints(m) {
		if k := b.Help().Key; k == "e" || k == "N" || k == "p" {
			t.Fatalf("picker footer offers %q, which it ignores", k)
		}
	}
}
