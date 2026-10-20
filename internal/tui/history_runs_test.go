package tui

import (
	"context"
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
