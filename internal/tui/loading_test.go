package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// newUndrivenModel builds a model the way newTestModel does, but without
// calling Init through drive — the caller wants to inspect state between
// individual messages of a load, not after the whole thing has settled.
func newUndrivenModel(b *fakeBackend, opts Options) *Model {
	if opts.RefreshInterval == 0 {
		opts.RefreshInterval = -1
	}
	m := New(context.Background(), b, opts)
	m.width, m.height = 140, 30
	m.filter.Cursor.SetMode(cursor.CursorStatic)
	return m
}

// firstOfType returns the first message of type T in msgs, and whether one
// was found.
func firstOfType[T any](msgs []tea.Msg) (T, bool) {
	var zero T
	for _, msg := range msgs {
		if t, ok := msg.(T); ok {
			return t, true
		}
	}
	return zero, false
}

// TestLoadPrioritizesTheVisibleKindsGroup checks that Init requests the
// currently-selected kind's fetch group before anything else — the "the
// visible resource kind is requested ... before unrelated kinds" half of
// issue #29.
func TestLoadPrioritizesTheVisibleKindsGroup(t *testing.T) {
	cases := []struct {
		kind string
		want vsphere.FetchGroup
	}{
		{"", vsphere.GroupVMs}, // default kind is VM
		{"vm", vsphere.GroupVMs},
		{"template", vsphere.GroupVMs}, // shares the same call as VM
		{"host", vsphere.GroupHosts},
		{"cluster", vsphere.GroupClusters},
		{"datastore", vsphere.GroupDatastores},
		{"network", vsphere.GroupNetworks},
	}
	for _, tc := range cases {
		b := &fakeBackend{
			contexts:    []*config.Context{ctx("prod", "https://vcsa.prod.internal")},
			inventories: map[string]*vsphere.Inventory{"prod": inventoryFor("prod")},
		}
		m := newUndrivenModel(b, Options{Current: "prod", Kind: tc.kind})

		beginMsgs := collect(m.Init())
		begin, ok := firstOfType[beginInventoryMsg](beginMsgs)
		if !ok {
			t.Fatalf("kind %q: Init did not produce a beginInventoryMsg", tc.kind)
		}
		_, cmd := m.Update(begin)
		groupMsgs := collect(cmd)
		got, ok := firstOfType[groupMsg](groupMsgs)
		if !ok {
			t.Fatalf("kind %q: the connect step did not dispatch exactly one fetch group", tc.kind)
		}
		if got.group != tc.want {
			t.Errorf("kind %q: priority group = %s, want %s", tc.kind, got.group, tc.want)
		}
		// Nothing else should have been requested yet — the connect step
		// dispatches the priority group alone.
		if n := len(groupMsgs); n != 1 {
			t.Errorf("kind %q: connect step dispatched %d groups, want exactly 1", tc.kind, n)
		}
	}
}

// TestPriorityGroupRendersBeforeTheRestLand proves the practical payoff of
// prioritizing: the visible tab has real rows on screen as soon as its own
// group lands, while the other four are still outstanding.
func TestPriorityGroupRendersBeforeTheRestLand(t *testing.T) {
	b := &fakeBackend{
		contexts:    []*config.Context{ctx("prod", "https://vcsa.prod.internal")},
		inventories: map[string]*vsphere.Inventory{"prod": inventoryFor("prod")},
	}
	m := newUndrivenModel(b, Options{Current: "prod"}) // default kind: VM

	begin, ok := firstOfType[beginInventoryMsg](collect(m.Init()))
	if !ok {
		t.Fatal("Init did not produce a beginInventoryMsg")
	}
	_, cmd := m.Update(begin)
	priority, ok := firstOfType[groupMsg](collect(cmd))
	if !ok {
		t.Fatal("connect step did not dispatch the priority group")
	}
	if priority.group != vsphere.GroupVMs {
		t.Fatalf("priority group = %s, want %s", priority.group, vsphere.GroupVMs)
	}
	m.Update(priority)

	if got := len(m.rows()); got == 0 {
		t.Error("VM rows are missing even though the priority group already landed")
	}
	st := m.byName["prod"]
	if !st.loading {
		t.Error("the load should still be in flight — four more groups are outstanding")
	}
	if len(st.inv.Hosts) != 0 {
		t.Error("hosts should not have arrived yet — only the priority group has landed")
	}
	if !st.kind(vsphere.KindVM).loaded || !st.kind(vsphere.KindTemplate).loaded {
		t.Error("the VM fetch group should mark both VM and template kinds loaded")
	}
	for _, kind := range []vsphere.Kind{vsphere.KindHost, vsphere.KindCluster, vsphere.KindVApp, vsphere.KindDatastore, vsphere.KindNetwork} {
		ks := st.kind(kind)
		if !ks.loading {
			t.Errorf("%s should have its own loading state after fan-out", kind)
		}
		if ks.loaded {
			t.Errorf("%s was marked loaded before its fetch result landed", kind)
		}
	}
}

// TestRemainingGroupsDispatchTogetherOncePriorityLands checks the other
// half: once the priority group lands, every other group is dispatched at
// once (as one batch), not one at a time.
func TestRemainingGroupsDispatchTogetherOncePriorityLands(t *testing.T) {
	b := &fakeBackend{
		contexts:    []*config.Context{ctx("prod", "https://vcsa.prod.internal")},
		inventories: map[string]*vsphere.Inventory{"prod": inventoryFor("prod")},
	}
	m := newUndrivenModel(b, Options{Current: "prod"})

	begin, _ := firstOfType[beginInventoryMsg](collect(m.Init()))
	_, cmd := m.Update(begin)
	priority, _ := firstOfType[groupMsg](collect(cmd))
	_, fanOutCmd := m.Update(priority)

	remaining := collect(fanOutCmd)
	if len(remaining) != len(vsphere.AllGroups)-1 {
		t.Fatalf("fan-out dispatched %d groups, want %d", len(remaining), len(vsphere.AllGroups)-1)
	}
	seen := map[vsphere.FetchGroup]bool{priority.group: true}
	for _, msg := range remaining {
		gm, ok := msg.(groupMsg)
		if !ok {
			t.Fatalf("fan-out produced a non-groupMsg: %T", msg)
		}
		if seen[gm.group] {
			t.Errorf("group %s was dispatched more than once", gm.group)
		}
		seen[gm.group] = true
	}
	for _, g := range vsphere.AllGroups {
		if !seen[g] {
			t.Errorf("group %s was never dispatched", g)
		}
	}
}

// TestFailedGroupPreservesOnlyItsOwnStaleData is the per-kind half of
// stale-while-revalidate: a refresh where hosts fails but everything else
// succeeds must keep the old hosts (and report the new error) without
// touching any other kind's fresh data.
func TestFailedGroupPreservesOnlyItsOwnStaleData(t *testing.T) {
	b := twoHealthy()
	m := newTestModel(t, b, Options{Current: "prod"})
	st := m.byName["prod"]
	staleHosts := st.inv.Hosts
	if len(staleHosts) == 0 {
		t.Fatal("test fixture has no hosts to go stale")
	}
	oldHostLoadedAt := st.kind(vsphere.KindHost).loadedAt
	oldVMLoadedAt := st.kind(vsphere.KindVM).loadedAt
	oldBundleLoadedAt := st.loadedAt
	if oldHostLoadedAt.IsZero() || oldVMLoadedAt.IsZero() {
		t.Fatal("successful groups must have per-kind load timestamps")
	}

	b.failures = nil // per-context whole-load failure is unused here
	orig := b.inventories["prod"]
	changed := *orig
	changed.VMs = append([]vsphere.VM(nil), orig.VMs...)
	changed.VMs[0].Name = "renamed-by-refresh"
	changed.Errors = []vsphere.InventoryError{{Kind: vsphere.KindHost, Message: "permission denied"}}
	b.inventories["prod"] = &changed

	// fakeInventoryHandle.FetchGroup slices straight from the fixture
	// Inventory, including the injected host error, so a plain forced
	// reload through the normal path reproduces a partial failure exactly
	// the way vsphere.Client.FetchGroup would report one.
	drive(t, m, tea.Batch(m.reload(false)...))

	st = m.byName["prod"]
	if len(st.inv.Hosts) != len(staleHosts) || st.inv.Hosts[0].Name != staleHosts[0].Name {
		t.Errorf("hosts after a failed refresh = %v, want the stale %v kept", st.inv.Hosts, staleHosts)
	}
	if msg, failed := st.inv.ErrorFor(vsphere.KindHost); !failed || msg != "permission denied" {
		t.Errorf("ErrorFor(host) = (%q, %v), want (\"permission denied\", true)", msg, failed)
	}
	found := false
	for _, vm := range st.inv.VMs {
		if vm.Name == "renamed-by-refresh" {
			found = true
		}
	}
	if !found {
		t.Error("VMs should have refreshed normally despite hosts failing")
	}
	if got := st.kind(vsphere.KindHost).loadedAt; !got.Equal(oldHostLoadedAt) {
		t.Errorf("failed host refresh changed its last-success timestamp from %v to %v", oldHostLoadedAt, got)
	}
	if got := st.kind(vsphere.KindVM).loadedAt; !got.After(oldVMLoadedAt) {
		t.Errorf("successful VM refresh did not advance its last-success timestamp: old=%v new=%v", oldVMLoadedAt, got)
	}
	if got := st.loadedAt; !got.Equal(oldBundleLoadedAt) {
		t.Errorf("partial refresh changed the complete-bundle timestamp from %v to %v", oldBundleLoadedAt, got)
	}
	if !st.kind(vsphere.KindHost).loaded || st.kind(vsphere.KindHost).loading {
		t.Error("failed host refresh should retain loaded data while clearing only its loading state")
	}
	if st.kind(vsphere.KindHost).err == nil {
		t.Error("failed host refresh did not retain its per-kind error")
	}
	st.kind(vsphere.KindHost).loadedAt = time.Now().Add(-time.Minute)
	st.loadedAt = time.Now()
	if !m.refreshDue(st, true) {
		t.Error("a stale host kind was hidden by the fresh complete-bundle timestamp")
	}
}

// TestEditBeforePriorityGroupLandsRetriesWithoutDeadlock reproduces the
// narrowest race in the begin→priority→fan-out progression: an edit landing
// while only the connect/index step's priority fetch is in flight (the
// other four groups were never dispatched at all yet). The retry must not
// wait for stragglers that will never arrive.
func TestEditBeforePriorityGroupLandsRetriesWithoutDeadlock(t *testing.T) {
	b := &fakeBackend{
		contexts:    []*config.Context{ctx("prod", "https://vcsa.prod.internal")},
		inventories: map[string]*vsphere.Inventory{"prod": inventoryFor("prod")},
	}
	m := newUndrivenModel(b, Options{Current: "prod"})

	begin, _ := firstOfType[beginInventoryMsg](collect(m.Init()))
	_, cmd := m.Update(begin)
	priority, ok := firstOfType[groupMsg](collect(cmd))
	if !ok {
		t.Fatal("connect step did not dispatch the priority group")
	}

	st := m.byName["prod"]
	if st.outstanding != 1 || !st.awaitingPriority {
		t.Fatalf("test setup: outstanding=%d awaitingPriority=%v, want 1/true before the edit", st.outstanding, st.awaitingPriority)
	}

	// Simulate an edit landing right now: a new *config.Context for the same
	// name, the way syncContexts installs one after a save.
	edited := ctx("prod", "https://vcsa.prod-edited.internal")
	st.cc = edited
	b.inventories["prod"] = inventoryFor("prod-edited")

	// The stale priority message lands.
	_, retryCmd := m.Update(priority)
	if st.loading != true {
		t.Fatal("the edit's retry should have started immediately — nothing else was ever in flight")
	}
	if st.inv != nil {
		t.Error("the stale priority group's data must not have been merged in")
	}

	// Drive the retry to completion and confirm it actually talks to the
	// edited endpoint (via the fresh fixture registered under it).
	drive(t, m, retryCmd)
	st = m.byName["prod"]
	if st.cc != edited {
		t.Fatal("test invariant broken: st.cc changed again unexpectedly")
	}
	if st.inv == nil || len(st.inv.VMs) == 0 {
		t.Error("the retried load should have populated inventory from the edited context")
	}
}

// TestEditDuringFanOutWaitsForEveryStraggler covers the wider window: an
// edit landing after the priority group already fanned the other four out,
// so four messages are genuinely in flight for the abandoned load. The
// retry must wait for all four, not fire after the first one back.
func TestEditDuringFanOutWaitsForEveryStraggler(t *testing.T) {
	b := &fakeBackend{
		contexts:    []*config.Context{ctx("prod", "https://vcsa.prod.internal")},
		inventories: map[string]*vsphere.Inventory{"prod": inventoryFor("prod")},
	}
	m := newUndrivenModel(b, Options{Current: "prod"})

	begin, _ := firstOfType[beginInventoryMsg](collect(m.Init()))
	_, cmd := m.Update(begin)
	priority, _ := firstOfType[groupMsg](collect(cmd))
	_, fanOutCmd := m.Update(priority)
	stragglers := collect(fanOutCmd)
	if len(stragglers) != len(vsphere.AllGroups)-1 {
		t.Fatalf("test setup: %d groups in flight, want %d", len(stragglers), len(vsphere.AllGroups)-1)
	}

	st := m.byName["prod"]
	if st.outstanding != len(stragglers) {
		t.Fatalf("test setup: outstanding=%d, want %d", st.outstanding, len(stragglers))
	}

	edited := ctx("prod", "https://vcsa.prod-edited.internal")
	st.cc = edited
	b.inventories["prod"] = inventoryFor("prod-edited")

	for i, msg := range stragglers {
		_, retryCmd := m.Update(msg)
		last := i == len(stragglers)-1
		if !last {
			if !st.loading || st.cc != edited {
				t.Fatalf("straggler %d/%d: retry fired early (outstanding=%d)", i+1, len(stragglers), st.outstanding)
			}
			if retryCmd != nil {
				t.Errorf("straggler %d/%d: expected no command yet, got one", i+1, len(stragglers))
			}
			continue
		}
		if retryCmd == nil {
			t.Fatal("the last straggler should have started the retry")
		}
		drive(t, m, retryCmd)
	}

	st = m.byName["prod"]
	if st.inv == nil || len(st.inv.VMs) == 0 {
		t.Error("the retried load should have populated inventory from the edited context")
	}
}

// TestSearchSeesDataAsSoonAsItLands checks that the estate-wide search
// treats a context as searched the moment its first group lands, not only
// once the whole load finishes — no longer having to wait for the whole
// bundle before answering at all.
//
// It is a coarser promise than the issue's ideal of reporting each
// still-loading kind separately (a context here is either "not yet
// searched" or "searched", not "searched for VMs but not yet for hosts");
// getting that granular would need searchState to track completeness per
// kind per context, which is a natural follow-up rather than something this
// change needs to get right on its own.
func TestSearchSeesDataAsSoonAsItLands(t *testing.T) {
	b := &fakeBackend{
		contexts:    []*config.Context{ctx("prod", "https://vcsa.prod.internal")},
		inventories: map[string]*vsphere.Inventory{"prod": inventoryFor("prod")},
	}
	m := newUndrivenModel(b, Options{Current: "prod"})

	begin, _ := firstOfType[beginInventoryMsg](collect(m.Init()))
	_, cmd := m.Update(begin)
	priority, _ := firstOfType[groupMsg](collect(cmd))
	m.Update(priority)

	st := m.byName["prod"]
	if !st.loading {
		t.Fatal("test setup: the load should still be in flight — four groups are outstanding")
	}

	// Before the fan-out lands, the context already has the priority
	// group's data, so it must count as searched rather than missing.
	search := m.ensureSearch("app-01") // a VM name from inventoryFor
	if search.searched != 1 {
		t.Errorf("searched = %d, want 1 — the priority group already landed", search.searched)
	}
	if len(search.missing) != 0 {
		t.Errorf("missing = %v, want none", search.missing)
	}
	if len(search.rows) == 0 {
		t.Error("search should already find the VM the priority group brought in")
	}
	if got, want := len(search.incomplete), len(vsphere.AllKinds)-2; got != want {
		t.Fatalf("incomplete kinds = %d, want %d after only the VM/template group landed", got, want)
	}
	for _, incomplete := range search.incomplete {
		if !incomplete.loading || incomplete.reason != "still loading" {
			t.Errorf("incomplete search state for %s/%s = loading:%v reason:%q, want still loading", incomplete.context.cc.Name, incomplete.kind, incomplete.loading, incomplete.reason)
		}
	}
	m.mode = modeSearch
	out := m.View()
	for _, kind := range []vsphere.Kind{vsphere.KindHost, vsphere.KindCluster, vsphere.KindVApp, vsphere.KindDatastore, vsphere.KindNetwork} {
		want := string(kind) + " incomplete: still loading"
		if !strings.Contains(out, want) {
			t.Errorf("search view does not report %s as still loading:\n%s", kind, out)
		}
	}
}
