package tui

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// vmObservation is the shorthand these tests build inventory with.
func vmObservation(name string, cpu int32, memoryMB int64) assessment.Observation {
	return assessment.Observation{VCenterID: "vc-1", Context: "prod", VM: vsphere.VM{
		ID: "vm-" + name, Name: name, InstanceUUID: "uuid-" + name,
		PowerState: "poweredOn", Host: "esx-01", CPU: cpu, MemoryMB: memoryMB,
	}}
}

// twoRunStore saves base and target inventories one hour apart and returns the
// store, so a test can say what changed rather than how the ledger records it.
func twoRunStore(t *testing.T, base, target []assessment.Observation) *assessment.Store {
	t.Helper()
	store, err := assessment.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	cc := &config.Context{Name: "prod", Endpoint: "https://prod", Username: "user"}
	ctx := context.Background()
	when := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	for _, vms := range [][]assessment.Observation{base, target} {
		run, err := store.StartRun(ctx, "test", []*config.Context{cc}, when)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SaveContext(ctx, run.ID, assessment.ContextResult{Name: "prod", VCenterID: "vc-1", Status: "success", VMs: vms}, when); err != nil {
			t.Fatal(err)
		}
		if _, err := store.FinishRun(ctx, run.ID, when); err != nil {
			t.Fatal(err)
		}
		when = when.Add(time.Hour)
	}
	return store
}

// TestScopeStreamRanksBlockersAboveChurn pins the reordering the whole pane
// exists for: a vanished VM is a decision someone has to make before a
// cutover, and it must sit above a rename however many renames a run
// produced. The old pane ordered by object name, so a blocker could sit
// eighty rows below the fold.
func TestScopeStreamRanksBlockersAboveChurn(t *testing.T) {
	base := []assessment.Observation{vmObservation("aaa-renamed", 2, 4096), vmObservation("zzz-doomed", 2, 4096)}
	target := []assessment.Observation{vmObservation("aaa-renamed", 2, 4096)}
	target[0].VM.Name = "aaa-renamed-now"
	store := twoRunStore(t, base, target)
	m := newTestModel(t, twoHealthy(), Options{Assessment: &assessment.Service{Store: store}})
	press(t, m, "H")

	rows := m.scopeRows()
	if len(rows) < 2 {
		t.Fatalf("stream has %d rows, want the vanished VM and the rename: %+v", len(rows), rows)
	}
	if rows[0].impact != impactBlocks || rows[0].object != "zzz-doomed" {
		t.Fatalf("first row is %q (%s), want the vanished VM ranked first", rows[0].object, rows[0].impact)
	}
	if rows[len(rows)-1].impact != impactChurn {
		t.Fatalf("last row is %s, want churn ranked last: %+v", rows[len(rows)-1].impact, rows)
	}
}

// TestScopeStreamRollsUpIdenticalChangesButNeverBlockers pins both halves of
// the grouping rule. Nine VMs given the same memory are one line, because
// nine identical lines are noise; nine vanished VMs stay nine lines, because
// a blocker you have to expand to name is a blocker nobody acts on.
func TestScopeStreamRollsUpIdenticalChangesButNeverBlockers(t *testing.T) {
	var base, target []assessment.Observation
	for i := 0; i < 4; i++ {
		name := "k8s-worker-0" + strconv.Itoa(i)
		base = append(base, vmObservation(name, 2, 4096))
		target = append(target, vmObservation(name, 2, 8192))
	}
	for i := 0; i < 3; i++ {
		base = append(base, vmObservation("legacy-0"+strconv.Itoa(i), 2, 4096))
	}
	store := twoRunStore(t, base, target)
	m := newTestModel(t, twoHealthy(), Options{Assessment: &assessment.Service{Store: store}})
	press(t, m, "H")

	rows := m.scopeRows()
	var grouped, blockers int
	for _, r := range rows {
		if r.impact == impactBlocks {
			blockers++
			if r.grouped() {
				t.Fatalf("a blocker was rolled up: %+v", r)
			}
		}
		if r.grouped() {
			grouped++
			if len(r.members) != 4 {
				t.Fatalf("group holds %d members, want the 4 resized VMs: %+v", len(r.members), r)
			}
			if !strings.Contains(r.object, "k8s-worker") || !strings.Contains(r.object, "4 VMs") {
				t.Fatalf("group label %q names neither the shared prefix nor the count", r.object)
			}
			if r.impact != impactSizing {
				t.Fatalf("a memory change was classed %s, want sizing", r.impact)
			}
		}
	}
	if blockers != 3 {
		t.Fatalf("stream has %d blocker rows, want one per vanished VM", blockers)
	}
	if grouped != 1 {
		t.Fatalf("stream has %d grouped rows, want the one resize group: %+v", grouped, rows)
	}
}

// TestScopeInspectorNamesGroupMembers pins what makes a rolled-up row usable:
// "4 VMs" is only actionable once the inspector says which four.
func TestScopeInspectorNamesGroupMembers(t *testing.T) {
	var base, target []assessment.Observation
	for i := 0; i < 4; i++ {
		name := "k8s-worker-0" + strconv.Itoa(i)
		base = append(base, vmObservation(name, 2, 4096))
		target = append(target, vmObservation(name, 4, 4096))
	}
	store := twoRunStore(t, base, target)
	m := newTestModel(t, twoHealthy(), Options{Assessment: &assessment.Service{Store: store}})
	press(t, m, "H")

	rows := m.scopeRows()
	if len(rows) != 1 || !rows[0].grouped() {
		t.Fatalf("want one grouped row, got %+v", rows)
	}
	inspector := strings.Join(m.scopeInspector(rows[0]), "\n")
	for i := 0; i < 4; i++ {
		if want := "k8s-worker-0" + strconv.Itoa(i); !strings.Contains(inspector, want) {
			t.Fatalf("inspector does not name group member %q:\n%s", want, inspector)
		}
	}
	if !strings.Contains(inspector, "4 objects") {
		t.Fatalf("inspector does not count the group:\n%s", inspector)
	}
}

// TestImpactFilterNarrowsAndClears pins the number keys: each one narrows the
// stream to a class, and the same key — or 0 — lets you back out of it.
func TestImpactFilterNarrowsAndClears(t *testing.T) {
	base := []assessment.Observation{vmObservation("keeper", 2, 4096), vmObservation("doomed", 2, 4096)}
	target := []assessment.Observation{vmObservation("keeper", 4, 4096), vmObservation("newcomer", 2, 4096)}
	store := twoRunStore(t, base, target)
	m := newTestModel(t, twoHealthy(), Options{Assessment: &assessment.Service{Store: store}})
	press(t, m, "H")

	if got := len(m.scopeRows()); got != 3 {
		t.Fatalf("unfiltered stream has %d rows, want 3: %+v", got, m.scopeRows())
	}
	press(t, m, "1")
	rows := m.scopeRows()
	if len(rows) != 1 || rows[0].object != "doomed" {
		t.Fatalf("blockers filter returned %+v", rows)
	}
	press(t, m, "1")
	if got := len(m.scopeRows()); got != 3 {
		t.Fatalf("pressing the same filter twice did not clear it: %d rows", got)
	}
	press(t, m, "3")
	rows = m.scopeRows()
	if len(rows) != 1 || rows[0].object != "newcomer" {
		t.Fatalf("growth filter returned %+v", rows)
	}
	press(t, m, "0")
	if m.impactFilter != impactAll {
		t.Fatalf("0 did not clear the filter: %s", m.impactFilter)
	}
}

// TestCoverageMatrixNamesTheDarkVCenterAndClipFixesIt is the estate-scale
// case this redesign was drawn for. The diff engine refuses to compare a
// vCenter one side never collected — so the counts are honest, but the
// comparison quietly stopped being estate-wide and the old pane said so only
// in a line of prose. The matrix draws which site was dark in which run, and
// "c" moves the baseline to the newest run that shares the target's coverage,
// so the span becomes a like-for-like comparison again.
func TestCoverageMatrixNamesTheDarkVCenterAndClipFixesIt(t *testing.T) {
	store, err := assessment.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	prod := &config.Context{Name: "prod", Endpoint: "https://prod", Username: "user"}
	edge := &config.Context{Name: "edge-vc", Endpoint: "https://edge", Username: "user"}
	edgeVM := assessment.Observation{VCenterID: "vc-edge", Context: "edge-vc", VM: vsphere.VM{ID: "vm-e1", Name: "edge-billing", InstanceUUID: "uuid-e1"}}

	// Run 2 could not reach edge-vc; runs 1 and 3 saw the whole estate. The
	// newest pair — the pane's default span — is therefore the one that
	// silently compares a single site.
	when := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		contexts := []*config.Context{prod, edge}
		if i == 1 {
			contexts = []*config.Context{prod}
		}
		run, err := store.StartRun(ctx, "test", contexts, when)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SaveContext(ctx, run.ID, assessment.ContextResult{Name: "prod", VCenterID: "vc-prod", Status: "success", VMs: []assessment.Observation{vmObservation("billing", 2, 4096)}}, when); err != nil {
			t.Fatal(err)
		}
		if i != 1 {
			if err := store.SaveContext(ctx, run.ID, assessment.ContextResult{Name: "edge-vc", VCenterID: "vc-edge", Status: "success", VMs: []assessment.Observation{edgeVM}}, when); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.FinishRun(ctx, run.ID, when); err != nil {
			t.Fatal(err)
		}
		when = when.Add(time.Hour)
	}

	m := newTestModel(t, twoHealthy(), Options{Assessment: &assessment.Service{Store: store}})
	press(t, m, "H")
	if m.historyCoverage == nil {
		t.Fatal("coverage was never loaded for the run axis")
	}
	matrix := strings.Join(m.viewCoverage(m.width), "\n")
	for _, want := range []string{"edge-vc", "prod", "✕", "●", "clip"} {
		if !strings.Contains(matrix, want) {
			t.Fatalf("coverage matrix missing %q:\n%s", want, matrix)
		}
	}
	if m.changeDiff == nil || m.changeDiff.Base.ID != 2 {
		t.Fatalf("expected the default span to start at the narrow run: %+v", m.changeDiff)
	}
	if !contextExcluded(m.changeDiff, "edge-vc") {
		t.Fatal("expected the default span to have quietly excluded edge-vc")
	}

	press(t, m, "c")
	if m.changeDiff.Base.ID != 1 {
		t.Fatalf("clip did not move the baseline to the run sharing the target's coverage: base=%d", m.changeDiff.Base.ID)
	}
	if contextExcluded(m.changeDiff, "edge-vc") {
		t.Fatalf("clipped span still excludes edge-vc: %+v", m.changeDiff.Coverage)
	}
}

// contextExcluded reports whether the diff had to leave one vCenter out of
// the comparison entirely.
func contextExcluded(d *assessment.Diff, name string) bool {
	for _, c := range d.Coverage {
		if c.Context == name {
			return true
		}
	}
	return false
}

// TestCoverageCollapsesWhenEveryRunSawEverything pins the other half of the
// matrix's job: rows are expensive on a terminal, so full coverage is one
// line and the space goes to the changes instead.
func TestCoverageCollapsesWhenEveryRunSawEverything(t *testing.T) {
	store := twoRunStore(t, []assessment.Observation{vmObservation("billing", 2, 4096)}, []assessment.Observation{vmObservation("billing", 4, 4096)})
	m := newTestModel(t, twoHealthy(), Options{Assessment: &assessment.Service{Store: store}})
	press(t, m, "H")
	lines := m.viewCoverage(m.width)
	if len(lines) != 1 || !strings.Contains(lines[0], "full coverage") {
		t.Fatalf("uniform coverage should collapse to one line, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
}

// TestScrubberWindowsRatherThanCompresses pins the narrow-terminal rule: the
// axis shows fewer runs at a readable pitch instead of the same runs crushed
// to a column each, and both ends of the comparison stay on it.
func TestScrubberWindowsRatherThanCompresses(t *testing.T) {
	runs := make([]assessment.Run, 40)
	for i := range runs {
		runs[i] = assessment.Run{ID: int64(40 - i), Status: assessment.RunComplete}
	}
	start, count := scrubWindow(runs, 0, 80)
	if count < 5 || count > 15 {
		t.Fatalf("80 columns drew %d cells, want a readable handful", count)
	}
	if start != 0 {
		t.Fatalf("window start=%d, want the newest runs", start)
	}
	wide, wideCount := scrubWindow(runs, 0, 200)
	if wideCount <= count {
		t.Fatalf("a wider terminal drew %d cells, not more than %d", wideCount, count)
	}
	if wide != 0 {
		t.Fatalf("wide window start=%d, want the newest runs", wide)
	}
	if _, capped := scrubWindow(runs[:3], 0, 200); capped != 3 {
		t.Fatalf("axis drew %d cells for 3 runs", capped)
	}
}

// TestScrubWindowFollowsTheHandles pins that an end moved beyond the window
// scrolls the window rather than disappearing off it.
func TestScrubWindowFollowsTheHandles(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{})
	m.width = 80
	for i := 0; i < 30; i++ {
		m.runs = append(m.runs, assessment.Run{ID: int64(30 - i), Status: assessment.RunComplete})
	}
	m.targetRun, m.baseRun = 30, 2
	m.centreScrubWindow()
	start, count := scrubWindow(m.runs, m.scrubOffset, m.width)
	oldest := m.runIndex(m.baseRun)
	if oldest < start || oldest >= start+count {
		t.Fatalf("baseline at index %d is outside the window [%d,%d)", oldest, start, start+count)
	}
}

// TestCondenseCoverageNotesFoldsPerKindGaps pins the fix for a header that
// used to spend ten lines on the same fact: a run predating a collection
// reports one note per kind, and ten of them pushed the changes off screen.
func TestCondenseCoverageNotesFoldsPerKindGaps(t *testing.T) {
	notes := []string{
		"cluster collection was not recorded in baseline",
		"cluster collection was not recorded in target",
		"datastore collection was not recorded in baseline",
		"datastore collection was not recorded in target",
		"host collection was not recorded in baseline",
	}
	got := condenseCoverageNotes(notes)
	if len(got) != 2 {
		t.Fatalf("condensed to %d lines, want one per side:\n%s", len(got), strings.Join(got, "\n"))
	}
	if !strings.Contains(got[0], "baseline") || !strings.Contains(got[0], "cluster, datastore, host") {
		t.Fatalf("baseline line does not name its kinds: %q", got[0])
	}
	if !strings.Contains(got[1], "target") || !strings.Contains(got[1], "cluster, datastore") {
		t.Fatalf("target line does not name its kinds: %q", got[1])
	}
}

// TestStaleSnapshotIsABlocker pins the one classification that depends on
// evidence the row summary throws away: a snapshot's actual age. A snapshot
// taken this morning is routine; one that has been growing for six weeks has
// to be consolidated before the VM can move.
func TestStaleSnapshotIsABlocker(t *testing.T) {
	d := &assessment.Diff{
		Snapshots: []assessment.SnapshotChange{
			{Kind: "created", VMName: "db-primary", Context: "prod", Name: "pre-patch"},
			{Kind: "created", VMName: "web-01", Context: "prod", Name: "quick"},
		},
		SnapshotAges: []assessment.SnapshotAge{
			{VMName: "db-primary", Name: "pre-patch", Age: 31 * 24 * time.Hour},
			{VMName: "web-01", Name: "quick", Age: 2 * time.Hour},
		},
	}
	rows := classifyDiff(d)
	if len(rows) != 2 {
		t.Fatalf("classified %d rows, want 2", len(rows))
	}
	if rows[0].impact != impactBlocks {
		t.Fatalf("a 31-day snapshot was classed %s, want blocks", rows[0].impact)
	}
	if !strings.Contains(rows[0].row.detail, "31d") {
		t.Fatalf("stale snapshot row does not say how old it is: %q", rows[0].row.detail)
	}
	if rows[1].impact == impactBlocks {
		t.Fatalf("a two-hour snapshot was classed as a blocker")
	}
}

// TestScopeStreamFitsEightyColumns pins the width floor the pane targets: at
// 80 columns every band still renders, and no line runs past the terminal.
func TestScopeStreamFitsEightyColumns(t *testing.T) {
	base := []assessment.Observation{vmObservation("billing-primary-database", 2, 4096), vmObservation("doomed", 2, 4096)}
	target := []assessment.Observation{vmObservation("billing-primary-database", 8, 65536)}
	store := twoRunStore(t, base, target)
	m := newTestModel(t, twoHealthy(), Options{Assessment: &assessment.Service{Store: store}})
	m.width = 80
	press(t, m, "H")
	for _, line := range m.viewChanges() {
		if width := ansi.StringWidth(line); width > 80 {
			t.Fatalf("line is %d columns wide at an 80 column terminal: %q", width, line)
		}
	}
	view := strings.Join(m.viewChanges(), "\n")
	for _, want := range []string{"RUNS", "IMPACT", "blocks", "doomed"} {
		if !strings.Contains(view, want) {
			t.Fatalf("narrow Changes view is missing %q:\n%s", want, view)
		}
	}
}
