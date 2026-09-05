package tui

import (
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// TestCaptureKeepsTheSpinnerTurning covers the reported bug directly: the
// spinner beside "capturing…" stood still, because a capture sets its own
// flag rather than the loading one busy() was reading, so the very first
// tick ended the chain and no later tick was ever scheduled.
func TestCaptureKeepsTheSpinnerTurning(t *testing.T) {
	m := &Model{spin: spinner.New(spinner.WithSpinner(spinner.Dot))}
	m.capturing = true

	if !m.busy() {
		t.Fatal("a running capture does not count as busy, so the spinner will not be ticked")
	}
	_, cmd := m.Update(m.spin.Tick())
	if cmd == nil {
		t.Fatal("a spinner tick during a capture scheduled no successor; the spinner freezes on the first frame")
	}

	before := m.spin.View()
	for i := 0; i < len(spinner.Dot.Frames); i++ {
		_, cmd = m.Update(m.spin.Tick())
		if cmd == nil {
			t.Fatalf("the tick chain stopped after %d frames of a running capture", i+1)
		}
		if m.spin.View() != before {
			return
		}
	}
	t.Error("the spinner never advanced a frame across a whole cycle of ticks")
}

// TestCaptureStartsItsOwnSpinnerTick is the other half of the same bug. Even
// with busy() fixed, a capture begun while nothing else was loading has no
// tick in flight to inherit, so it has to start one itself.
func TestCaptureStartsItsOwnSpinnerTick(t *testing.T) {
	b := &fakeBackend{
		contexts:    []*config.Context{ctx("prod", "https://vcsa.prod.internal")},
		inventories: map[string]*vsphere.Inventory{"prod": inventoryFor("prod")},
	}
	store, err := assessment.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("open history store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	m := newTestModel(t, b, Options{Current: "prod", Assessment: &assessment.Service{Store: store}})

	cmd := m.captureCommand()
	if cmd == nil {
		t.Fatal("captureCommand returned nothing")
	}
	if !m.capturing {
		t.Fatal("captureCommand did not mark the model as capturing")
	}
	if _, ok := firstOfType[spinner.TickMsg](collect(cmd)); !ok {
		t.Error("captureCommand dispatched no spinner tick, so nothing turns the spinner it draws")
	}
}

// TestPagesRenderBeforeTheGroupCompletes is the payoff of the paged read:
// rows are on screen while the rest of the estate is still arriving.
func TestPagesRenderBeforeTheGroupCompletes(t *testing.T) {
	b := &fakeBackend{
		contexts:    []*config.Context{ctx("prod", "https://vcsa.prod.internal")},
		inventories: map[string]*vsphere.Inventory{"prod": inventoryFor("prod")},
	}
	m := newUndrivenModel(b, Options{Current: "prod"})

	begin, ok := firstOfType[beginInventoryMsg](collect(m.Init()))
	if !ok {
		t.Fatal("Init produced no beginInventoryMsg")
	}
	_, cmd := m.Update(begin)
	msgs := collect(cmd)

	page, ok := firstOfType[groupPageMsg](msgs)
	if !ok {
		t.Fatal("the priority fetch produced no page ahead of its result")
	}
	if len(m.rows()) != 0 {
		t.Fatalf("rows on screen before any page landed: %d", len(m.rows()))
	}
	m.Update(page)
	if len(m.rows()) == 0 {
		t.Fatal("a page landed and the table is still empty; pages are not being shown")
	}
}

// TestRefreshPagesDoNotEmptyTheTable guards the deliberate asymmetry: pages
// fill a table that has nothing in it, but a refresh already has a complete
// list up and must not replace it with a partial one on the way past.
func TestRefreshPagesDoNotEmptyTheTable(t *testing.T) {
	b := &fakeBackend{
		contexts:    []*config.Context{ctx("prod", "https://vcsa.prod.internal")},
		inventories: map[string]*vsphere.Inventory{"prod": inventoryFor("prod")},
	}
	m := newTestModel(t, b, Options{Current: "prod"})
	loaded := len(m.rows())
	if loaded == 0 {
		t.Fatal("the first load put no rows on screen")
	}

	st := m.byName["prod"]
	// One page of a refresh, carrying a single VM: applying it would leave
	// one row where the whole estate had been.
	m.Update(groupPageMsg{
		context:    "prod",
		cc:         st.cc,
		generation: st.generation,
		group:      vsphere.GroupVMs,
		inv:        &vsphere.Inventory{Context: "prod", VMs: []vsphere.VM{{ID: "vm-new", Name: "aaa-new"}}},
		pages:      nil,
	})
	if got := len(m.rows()); got != loaded {
		t.Errorf("a refresh page changed the table from %d rows to %d; a loaded kind must not show partial pages", loaded, got)
	}
}

// TestCursorStaysOnTheSameObjectAsRowsArrive checks that a list growing
// underneath the reader does not move the selection onto a different object.
func TestCursorStaysOnTheSameObjectAsRowsArrive(t *testing.T) {
	b := &fakeBackend{
		contexts:    []*config.Context{ctx("prod", "https://vcsa.prod.internal")},
		inventories: map[string]*vsphere.Inventory{"prod": inventoryFor("prod")},
	}
	m := newTestModel(t, b, Options{Current: "prod"})
	if len(m.rows()) < 2 {
		t.Skip("fixture has too few rows to move the cursor")
	}
	m.moveTo(len(m.rows()) - 1)
	selected, ok := m.currentRow()
	if !ok {
		t.Fatal("no row under the cursor")
	}

	st := m.byName["prod"]
	// A machine sorting above the selection appears. Held by index, the
	// cursor would slide onto its neighbour.
	m.preserveCursor(func() {
		st.inv.MergeGroup(vsphere.GroupVMs, &vsphere.Inventory{
			Context: "prod",
			VMs:     []vsphere.VM{{ID: "vm-aaa", Name: "aaa-first", Location: vsphere.Location{Context: "prod"}}},
		})
		st.invalidateRows()
	})

	now, ok := m.currentRow()
	if !ok {
		t.Fatal("no row under the cursor after the merge")
	}
	if now.key != selected.key {
		t.Errorf("cursor moved from %q to %q when a row was inserted above it", selected.key, now.key)
	}
}

// TestRowsAreCachedUntilTheInventoryChanges guards the render cost: an
// estate of thousands of machines was being flattened into rows on every
// frame, several times a second.
func TestRowsAreCachedUntilTheInventoryChanges(t *testing.T) {
	b := &fakeBackend{
		contexts:    []*config.Context{ctx("prod", "https://vcsa.prod.internal")},
		inventories: map[string]*vsphere.Inventory{"prod": inventoryFor("prod")},
	}
	m := newTestModel(t, b, Options{Current: "prod"})
	st := m.byName["prod"]

	first := st.rowsFor(vsphere.KindVM, false)
	second := st.rowsFor(vsphere.KindVM, false)
	if len(first) == 0 {
		t.Fatal("no VM rows to cache")
	}
	if &first[0] != &second[0] {
		t.Error("rowsFor rebuilt the rows instead of returning the cached slice")
	}

	st.invalidateRows()
	third := st.rowsFor(vsphere.KindVM, false)
	if len(third) > 0 && &first[0] == &third[0] {
		t.Error("rowsFor returned stale rows after the cache was invalidated")
	}

	// Sorting the model's view must never reach back into the cache.
	rows := m.rows()
	if len(rows) > 0 && len(third) > 0 && &rows[0] == &third[0] {
		t.Error("rows() handed out the cached slice itself; filtering or sorting it would corrupt the cache")
	}
}
