package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestChangesRunPickerSelectsAnotherBaseline(t *testing.T) {
	store, err := assessment.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cc := &config.Context{Name: "prod", Endpoint: "https://prod", Username: "user"}
	for i := 0; i < 3; i++ {
		when := time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC)
		run, err := store.StartRun(context.Background(), "test", []*config.Context{cc}, when)
		if err != nil {
			t.Fatal(err)
		}
		err = store.SaveContext(context.Background(), run.ID, assessment.ContextResult{
			Name: "prod", VCenterID: "vc-1", Status: "success",
			VMs: []assessment.Observation{{VCenterID: "vc-1", Context: "prod", VM: vsphere.VM{ID: "vm-1", Name: "billing"}}},
		}, when.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.FinishRun(context.Background(), run.ID, when.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	m := newTestModel(t, twoHealthy(), Options{Assessment: &assessment.Service{Store: store}})
	press(t, m, "H")
	if m.mode != modeChanges || m.targetRun == 0 || m.baseRun == 0 {
		t.Fatalf("changes state: mode=%v base=%d target=%d", m.mode, m.baseRun, m.targetRun)
	}
	press(t, m, "b", "down", "enter")
	if m.mode != modeChanges || m.baseRun == m.targetRun {
		t.Fatalf("picker did not select a distinct baseline: mode=%v base=%d target=%d", m.mode, m.baseRun, m.targetRun)
	}
	if m.baseRun != 1 {
		t.Fatalf("base run=%d, want oldest run 1", m.baseRun)
	}
}

func TestHistoryHeaderNamesTheSelectedPane(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{})
	m.runs = []assessment.Run{{ID: 42}}
	m.targetRun = 42
	m.mode = modeChanges

	m.historyPane = historyPaneChanges
	if got := m.viewChangesHeader(); !strings.Contains(got, "history  ·  Changes  ·  target #42") {
		t.Fatalf("changes header does not name the pane: %q", got)
	}
	m.historyPane = historyPaneTrends
	if got := m.viewChangesHeader(); !strings.Contains(got, "history  ·  Trends") || strings.Contains(got, "target #42") {
		t.Fatalf("trends header was mislabeled: %q", got)
	}
	m.historyPane = historyPaneRuns
	if got := m.viewChangesHeader(); !strings.Contains(got, "history  ·  Runs") || strings.Contains(got, "target #42") {
		t.Fatalf("runs header was mislabeled: %q", got)
	}
}

// TestHistoryCaptureKeyIsPaneIndependent pins the resolution of the old "n"
// collision: capture from every pane, and the note editor on "N".
func TestHistoryCaptureKeyIsPaneIndependent(t *testing.T) {
	store, err := assessment.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	m := newTestModel(t, twoHealthy(), Options{Assessment: &assessment.Service{Store: store}})
	m.mode = modeChanges
	// A populated run list is what used to make "n" ambiguous: the Runs pane
	// read it as "edit this run's note".
	m.runs = []assessment.Run{{ID: 1}, {ID: 2}}

	for _, pane := range []int{historyPaneChanges, historyPaneTrends, historyPaneRuns} {
		m.historyPane = pane
		m.runEditKind = ""
		m.historyErr = nil
		press(t, m, "n")
		if m.runEditKind != "" || m.mode == modeHistoryRunEdit {
			t.Fatalf("pane %d: \"n\" opened the %q editor instead of capturing", pane, m.runEditKind)
		}
		// The service carries no collector, so a capture that was actually
		// dispatched comes back with exactly this error. The note editor, the
		// branch "n" used to take here, reaches no service at all.
		if m.historyErr == nil || !strings.Contains(m.historyErr.Error(), "collector is not configured") {
			t.Fatalf("pane %d: \"n\" did not dispatch a capture (historyErr=%v)", pane, m.historyErr)
		}
	}
}

func TestHistoryNoteKeyOpensTheNoteEditor(t *testing.T) {
	store, err := assessment.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	m := newTestModel(t, twoHealthy(), Options{Assessment: &assessment.Service{Store: store}})
	m.mode = modeChanges
	m.historyPane = historyPaneRuns
	m.runs = []assessment.Run{{ID: 7, Note: "quarter close"}}

	press(t, m, "N")
	if m.mode != modeHistoryRunEdit || m.runEditKind != "note" {
		t.Fatalf("\"N\" did not open the note editor: mode=%v kind=%q", m.mode, m.runEditKind)
	}
	if got := m.runEditInput.Value(); got != "quarter close" {
		t.Fatalf("note editor seeded with %q, want the run's existing note", got)
	}
}

// TestChangesFooterDescribesPanesNotKinds guards the label, not the key: the
// footer used to borrow NextTab/PrevTab and tell the operator "next kind" on a
// screen that has no kinds.
func TestChangesFooterDescribesPanesNotKinds(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{})
	m.mode = modeChanges
	for _, b := range defaultKeys().footerHints(m) {
		if strings.Contains(b.Help().Desc, "kind") {
			t.Fatalf("changes footer still advertises %q/%q", b.Help().Key, b.Help().Desc)
		}
	}
}
