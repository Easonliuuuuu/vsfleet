package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
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

// TestComparisonBarNamesBothRunsAndCoverageGap pins the fix for the
// Changes screen's loudest failure mode: a run that covered fewer vCenters
// than its baseline must say so by name, not leave "vanished" looking like
// mass deletion. It also pins that the name is the operator's context name,
// not the raw VCenterID diff.go used to leak into these messages.
func TestComparisonBarNamesBothRunsAndCoverageGap(t *testing.T) {
	store, err := assessment.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	prod := &config.Context{Name: "prod", Endpoint: "https://prod", Username: "user"}
	edge := &config.Context{Name: "edge-vc", Endpoint: "https://edge", Username: "user"}
	ctx := context.Background()

	base, err := store.StartRun(ctx, "test", []*config.Context{prod, edge}, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveContext(ctx, base.ID, assessment.ContextResult{Name: "prod", VCenterID: "vc-prod", Status: "success"}, base.StartedAt); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveContext(ctx, base.ID, assessment.ContextResult{Name: "edge-vc", VCenterID: "vc-edge", Status: "success"}, base.StartedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishRun(ctx, base.ID, base.StartedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateRun(ctx, strconv.FormatInt(base.ID, 10), assessment.RunMetadata{Label: "pre-maintenance", Pinned: true}); err != nil {
		t.Fatal(err)
	}

	// The target run only covers "prod" — the same narrower-capture scenario
	// that made "-96 vanished" look like an outage in the connected testbed.
	target, err := store.StartRun(ctx, "test", []*config.Context{prod}, time.Date(2026, 1, 1, 14, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveContext(ctx, target.ID, assessment.ContextResult{Name: "prod", VCenterID: "vc-prod", Status: "success"}, target.StartedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishRun(ctx, target.ID, target.StartedAt); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(t, twoHealthy(), Options{Assessment: &assessment.Service{Store: store}})
	press(t, m, "H")
	if m.mode != modeChanges || m.baseRun != base.ID || m.targetRun != target.ID {
		t.Fatalf("changes state: mode=%v base=%d target=%d", m.mode, m.baseRun, m.targetRun)
	}
	bar := strings.Join(m.viewComparisonBar(m.changeDiff), "\n")
	for _, want := range []string{
		"pre-maintenance", "📌", historyRunLabel(base.ID), historyRunLabel(target.ID),
		"2 vCenters", "1 vCenter", "+2h01m",
		"not compared: edge-vc",
	} {
		if !strings.Contains(bar, want) {
			t.Fatalf("comparison bar missing %q:\n%s", want, bar)
		}
	}
	if strings.Contains(bar, "vc-edge") || strings.Contains(bar, "vc-prod") {
		t.Fatalf("comparison bar leaked a raw VCenterID instead of its context name:\n%s", bar)
	}
}

// TestSwapKeyExchangesBaselineAndTarget pins "s" on the Changes pane: it
// swaps which run is baseline and which is target, and re-diffs immediately
// rather than leaving the two counts stale until the next unrelated update.
func TestSwapKeyExchangesBaselineAndTarget(t *testing.T) {
	store, err := assessment.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cc := &config.Context{Name: "prod", Endpoint: "https://prod", Username: "user"}
	for i := 0; i < 2; i++ {
		when := time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC)
		run, err := store.StartRun(context.Background(), "test", []*config.Context{cc}, when)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SaveContext(context.Background(), run.ID, assessment.ContextResult{Name: "prod", VCenterID: "vc-1", Status: "success"}, when); err != nil {
			t.Fatal(err)
		}
		if _, err := store.FinishRun(context.Background(), run.ID, when); err != nil {
			t.Fatal(err)
		}
	}
	m := newTestModel(t, twoHealthy(), Options{Assessment: &assessment.Service{Store: store}})
	press(t, m, "H")
	base, target := m.baseRun, m.targetRun
	press(t, m, "s")
	if m.baseRun != target || m.targetRun != base {
		t.Fatalf("swap did not exchange runs: base=%d target=%d, want base=%d target=%d", m.baseRun, m.targetRun, target, base)
	}
	if m.changeDiff == nil || m.changeDiff.Base.ID != target || m.changeDiff.Target.ID != base {
		t.Fatalf("swap did not re-diff: diff=%+v", m.changeDiff)
	}
}

func TestHistoryHeaderNamesTheSelectedPane(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{})
	m.runs = []assessment.Run{{ID: 42}}
	m.targetRun = 42
	m.mode = modeChanges

	// The run identity itself now lives on the comparison bar beneath the
	// header (see TestComparisonBarNamesBothRuns), so the header's job here
	// is only to name which pane is focused.
	m.historyPane = historyPaneChanges
	if got := m.viewChangesHeader(); !strings.Contains(got, "history  ·  Changes") {
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

// TestCaptureContextsIsScoped checks that a capture reads only the vCenter(s)
// on screen — the selected context alone, and every context once the
// all-vCenters view is on — rather than every configured vCenter regardless
// of scope.
func TestCaptureContextsIsScoped(t *testing.T) {
	store, err := assessment.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	m := newTestModel(t, twoHealthy(), Options{Current: "prod", Assessment: &assessment.Service{Store: store}})

	got := m.captureContexts()
	if len(got) != 1 || got[0].Name != "prod" {
		t.Fatalf("scoped capture contexts = %+v, want just prod", got)
	}

	m.allScope = true
	got = m.captureContexts()
	if len(got) != 2 {
		t.Fatalf("all-vCenters capture contexts = %+v, want both configured vCenters", got)
	}
}

// TestCaptureCredentialRequestIsScopedAndAttributed pins the fix for a
// capture that both connected to vCenters outside scope and opened an
// unattributed password overlay for them: credentialState now knows about a
// context's contextState.capturing flag, so a request naming an out-of-scope
// vCenter is deferred exactly like a quiet background refresh, while one
// naming the in-scope vCenter opens an overlay labelled with it.
func TestCaptureCredentialRequestIsScopedAndAttributed(t *testing.T) {
	store, err := assessment.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	m := newTestModel(t, twoHealthy(), Options{Current: "prod", Assessment: &assessment.Service{Store: store}})
	// captureCommand's own tea.Cmd is not run here — running it would resolve
	// the capture immediately (the service carries no collector) and clear
	// the very state this test inspects.
	if cmd := m.captureCommand(); cmd == nil {
		t.Fatal("captureCommand returned nil with an assessment service configured")
	}
	if !m.byName["prod"].capturing {
		t.Fatal("captureCommand did not mark the in-scope context as capturing")
	}
	if m.byName["customer-a"].capturing {
		t.Fatal("captureCommand marked an out-of-scope context as capturing")
	}

	outOfScope := credRequest{label: "customer-a", resp: make(chan credResult, 1)}
	m.Update(credRequestMsg{req: outOfScope})
	if m.credPrompt != nil {
		t.Fatal("a request for a vCenter outside the capture's scope opened an overlay")
	}
	select {
	case res := <-outOfScope.resp:
		if res.err == nil {
			t.Fatal("expected the out-of-scope request to be deferred, got a nil error")
		}
	default:
		t.Fatal("the out-of-scope request was never answered")
	}

	inScope := credRequest{label: "prod", resp: make(chan credResult, 1)}
	m.Update(credRequestMsg{req: inScope})
	if m.credPrompt == nil || m.credPrompt.label != "prod" {
		t.Fatalf("expected an overlay labelled %q for the captured vCenter, got %+v", "prod", m.credPrompt)
	}
	m.resolveCredPrompt(credResult{err: errPromptCanceled})
}

// TestCaptureCredentialGateClosesOnCompletion checks that a finished capture
// stops answering for the vCenters it covered — contextState.capturing is
// cleared once historyCaptureMsg lands, the same way a load's own gate closes
// in finishLoad.
func TestCaptureCredentialGateClosesOnCompletion(t *testing.T) {
	store, err := assessment.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	m := newTestModel(t, twoHealthy(), Options{Current: "prod", Assessment: &assessment.Service{Store: store}})
	if cmd := m.captureCommand(); cmd == nil {
		t.Fatal("captureCommand returned nil with an assessment service configured")
	}
	if !m.byName["prod"].capturing {
		t.Fatal("captureCommand did not mark the in-scope context as capturing")
	}

	m.Update(historyCaptureMsg{err: errors.New("capture failed")})
	if m.byName["prod"].capturing {
		t.Fatal("a finished capture left the context marked as capturing")
	}

	request := credRequest{label: "prod", resp: make(chan credResult, 1)}
	m.Update(credRequestMsg{req: request})
	if m.credPrompt != nil {
		t.Fatal("a request after the capture finished still opened an overlay")
	}
	select {
	case res := <-request.resp:
		if res.err == nil {
			t.Fatal("expected the post-capture request to be deferred, got a nil error")
		}
	default:
		t.Fatal("the post-capture request was never answered")
	}
}
