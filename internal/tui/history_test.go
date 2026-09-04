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
