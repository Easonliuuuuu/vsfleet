package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/contextops"
	"github.com/easonliuuuuu/vsfleet/internal/session"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// fakeBackend answers instantly from fixtures. The whole point of the Backend
// interface is that everything below is exercised here without a vCenter, a
// proxy or a certificate anywhere in sight.
//
// SaveContext, RemoveContext and TestContext apply to an in-memory context
// list rather than exercising contextops itself — that package has its own
// tests against a real simulated vCenter. What these tests are exercising is
// how the model reacts to the outcome: the sidebar rebuilding after a save,
// the form staying open on a failure, the last context's removal reopening
// setup.
type fakeBackend struct {
	contexts    []*config.Context
	inventories map[string]*vsphere.Inventory
	failures    map[string]error
	diagnoses   map[string]*vsphere.Diagnosis
	calls       map[string]int

	// testDiag, keyed by the input's context name, overrides what
	// TestContext and a tested SaveContext report; unset names get a passing
	// stub. saveErr forces SaveContext to fail outright, past any test.
	testDiag       map[string]*vsphere.Diagnosis
	saveErr        map[string]error
	discoverErr    error
	discoverThumb  string
	discoverSubj   string
	currentContext string
}

func (b *fakeBackend) Contexts() []*config.Context { return b.contexts }

func (b *fakeBackend) Inventory(_ context.Context, cc *config.Context) (*vsphere.Inventory, error) {
	if b.calls == nil {
		b.calls = map[string]int{}
	}
	b.calls[cc.Name]++
	if err, ok := b.failures[cc.Name]; ok {
		return nil, err
	}
	if inv, ok := b.inventories[cc.Name]; ok {
		return inv, nil
	}
	// A context with no fixture registered — a freshly saved context in the
	// form tests, say — connects successfully to an empty vCenter rather
	// than to nothing at all.
	return &vsphere.Inventory{Context: cc.Name}, nil
}

func (b *fakeBackend) Status(name string) (session.Status, bool) {
	return session.Status{Name: name}, false
}

func (b *fakeBackend) Diagnose(_ context.Context, cc *config.Context) *vsphere.Diagnosis {
	return b.diagnoses[cc.Name]
}

func (b *fakeBackend) diagnosisFor(name string) *vsphere.Diagnosis {
	if d, ok := b.testDiag[name]; ok {
		return d
	}
	return &vsphere.Diagnosis{
		Context: name,
		Checks:  []vsphere.Check{{Name: "Configuration valid", Status: vsphere.CheckPass}, {Name: "Authentication", Status: vsphere.CheckPass}},
	}
}

func (b *fakeBackend) TestContext(_ context.Context, in contextops.Input) (*config.Context, *vsphere.Diagnosis) {
	return contextops.Build(in), b.diagnosisFor(in.Name)
}

func (b *fakeBackend) SaveContext(_ context.Context, in contextops.Input, test bool) (*contextops.Result, error) {
	cc := contextops.Build(in)
	res := &contextops.Result{Context: cc}
	if err := cc.Validate(); err != nil {
		return res, err
	}
	if test {
		res.Diagnosis = b.diagnosisFor(in.Name)
		if !res.Diagnosis.OK() && !in.SaveOnTestFailure {
			return res, errors.New("connection test failed")
		}
	}
	if err, ok := b.saveErr[in.Name]; ok && err != nil {
		return res, err
	}
	replaced := false
	for i, existing := range b.contexts {
		if existing.Name == cc.Name {
			b.contexts[i] = cc
			replaced = true
			break
		}
	}
	if !replaced {
		b.contexts = append(b.contexts, cc)
	}
	if in.SetCurrent {
		b.currentContext = cc.Name
	}
	return res, nil
}

func (b *fakeBackend) RemoveContext(_ context.Context, name string, _ bool) (*config.Context, error) {
	for i, cc := range b.contexts {
		if cc.Name == name {
			b.contexts = append(b.contexts[:i:i], b.contexts[i+1:]...)
			if b.currentContext == name {
				b.currentContext = ""
			}
			return cc, nil
		}
	}
	return nil, fmt.Errorf("context %q not found", name)
}

func (b *fakeBackend) DiscoverThumbprint(_ context.Context, cc *config.Context) (sha256, sha1, subject string, notAfter time.Time, err error) {
	if b.discoverErr != nil {
		return "", "", "", time.Time{}, b.discoverErr
	}
	thumb := b.discoverThumb
	if thumb == "" {
		thumb = "AA:BB:CC:DD:EE:FF"
	}
	subj := b.discoverSubj
	if subj == "" {
		subj = cc.Host()
	}
	return thumb, thumb, subj, time.Now().AddDate(1, 0, 0), nil
}

func ctx(name, endpoint string) *config.Context {
	cc := &config.Context{Name: name, Endpoint: endpoint, Username: "operator@vsphere.local"}
	cc.Normalize()
	return cc
}

func inventoryFor(name string) *vsphere.Inventory {
	loc := func(kind, obj string) vsphere.Location {
		return vsphere.Location{Context: name, Datacenter: "Taipei", Path: "/Taipei/" + kind + "/" + obj}
	}
	return &vsphere.Inventory{
		Context: name,
		VMs: []vsphere.VM{
			{
				Location: loc("vm", "app-01"), ID: name + "-vm-1", Name: "app-01",
				PowerState: "poweredOn", CPU: 4, MemoryMB: 16384, GuestOS: "Ubuntu Linux (64-bit)",
				IPAddress: "10.20.0.11", Host: "esxi-01", Cluster: "compute", Folder: "/Apps",
				Datastores: []string{"nvme-01"}, StorageGB: 64, Annotation: "front end",
			},
			{
				Location: loc("vm", "build-runner-3"), ID: name + "-vm-2", Name: "build-runner-3",
				PowerState: "poweredOff", CPU: 8, MemoryMB: 32768, Host: "esxi-02", Cluster: "compute",
			},
		},
		Templates: []vsphere.VM{
			{
				Location: loc("vm", "ubuntu-24.04-golden"), ID: name + "-tpl-1", Name: "ubuntu-24.04-golden",
				IsTemplate: true, CPU: 2, MemoryMB: 4096, GuestOS: "Ubuntu Linux (64-bit)", StorageGB: 12,
			},
		},
		Hosts: []vsphere.Host{
			{
				Location: loc("host", "esxi-01"), ID: name + "-host-1", Name: "esxi-01",
				Cluster: "compute", PowerState: "poweredOn", ConnectionState: "connected",
				Vendor: "Dell Inc.", Model: "PowerEdge R750", Version: "8.0.3", Build: "24022515",
				CPUCores: 32, CPUThreads: 64, CPUMHz: 2400, MemoryMB: 524288,
				CPUUsageMHz: 18000, MemoryUsageMB: 262144, VMCount: 41,
			},
		},
		Clusters: []vsphere.Cluster{
			{
				Location: loc("host", "compute"), ID: name + "-cl-1", Name: "compute",
				Hosts: 4, EffectiveHost: 4, CPUCores: 128, TotalCPUMHz: 307200,
				TotalMemoryMB: 2097152, DRSEnabled: true, HAEnabled: true,
			},
		},
		Datastores: []vsphere.Datastore{
			{
				Location: loc("datastore", "nvme-01"), ID: name + "-ds-1", Name: "nvme-01",
				Type: "VMFS", Accessible: true,
				CapacityBytes: 8 << 40, FreeBytes: 2 << 40,
			},
		},
		Networks: []vsphere.Network{
			{
				Location: loc("network", "vlan-200"), ID: name + "-net-1", Name: "vlan-200",
				Type: "DistributedVirtualPortgroup", Accessible: true,
			},
		},
	}
}

func newTestModel(t *testing.T, b *fakeBackend, opts Options) *Model {
	t.Helper()
	m := New(context.Background(), b, opts)
	m.width, m.height = 140, 30
	// A blinking cursor schedules a real half-second timer, and these tests run
	// commands synchronously rather than through the Bubble Tea runtime.
	m.filter.Cursor.SetMode(cursor.CursorStatic)
	drive(t, m, m.Init())
	settleForm(m)
	return m
}

// settleForm strips the blink timer from every field of an open form, the
// same reason newTestModel does it for the filter: typing a character that
// moves the cursor position schedules a real ~500ms timer once per
// keystroke, which a synchronous test harness blocks on rather than letting
// run in the background the way the real Bubble Tea event loop would.
func settleForm(m *Model) {
	if m.form == nil {
		return
	}
	f := m.form
	for _, ti := range []*textinput.Model{
		&f.name, &f.endpoint, &f.username, &f.password,
		&f.datacenter, &f.proxyAddr, &f.proxyUser, &f.proxyPass, &f.thumbprint,
	} {
		ti.Cursor.SetMode(cursor.CursorStatic)
	}
}

// drive runs every command a model produced and feeds the resulting messages
// back in. Spinner ticks are dropped: they schedule themselves, and the test
// is interested in what the interface shows, not in how it animates.
func drive(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for _, msg := range collect(cmd) {
		if _, ok := msg.(spinner.TickMsg); ok {
			continue
		}
		drive(t, m, discard(m.Update(msg)))
	}
}

func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, collect(c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

func discard(_ tea.Model, cmd tea.Cmd) tea.Cmd { return cmd }

// press sends a key by name, the way a person would type it.
func press(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "left":
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		drive(t, m, discard(m.Update(msg)))
	}
}

// typeText sends a whole string as one key message, the way pasting or a
// fast typist would, rather than one press call per rune.
func typeText(t *testing.T, m *Model, s string) {
	t.Helper()
	drive(t, m, discard(m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})))
}

func twoHealthy() *fakeBackend {
	return &fakeBackend{
		contexts: []*config.Context{ctx("customer-a", "https://vcsa.customer-a.internal"), ctx("prod", "https://vcsa.prod.internal")},
		inventories: map[string]*vsphere.Inventory{
			"customer-a": inventoryFor("customer-a"),
			"prod":       inventoryFor("prod"),
		},
	}
}

// TestBrowseShowsOnlyTheSelectedContext checks that a single-context scope
// controls what the table displays, not what gets fetched: Init prefetches
// every configured context in the background (Section 4) precisely so that
// switching to customer-a later shows data immediately instead of a fresh
// spinner, but the table itself must still only show the selected context's
// rows until the reader asks for more.
func TestBrowseShowsOnlyTheSelectedContext(t *testing.T) {
	b := twoHealthy()
	m := newTestModel(t, b, Options{Current: "prod"})

	if got := b.calls["prod"]; got != 1 {
		t.Fatalf("prod fetched %d times, want 1", got)
	}
	if got := b.calls["customer-a"]; got != 1 {
		t.Errorf("customer-a fetched %d times, want 1 — Init prefetches every context", got)
	}
	out := m.View()
	if !strings.Contains(out, "app-01") {
		t.Errorf("VM list is missing app-01:\n%s", out)
	}
	// Both contexts' fake inventories are identical (same VM names), so the
	// count is what proves scope: merged, it would be 4; scoped to prod
	// alone, 2.
	if n := len(m.rows()); n != 2 {
		t.Errorf("a single-context scope must show only prod's rows, got %d", n)
	}
	// Both contexts belong in the sidebar even though only one is in scope:
	// the sidebar is the switcher.
	for _, name := range []string{"prod", "customer-a"} {
		if !strings.Contains(out, name) {
			t.Errorf("sidebar is missing %q:\n%s", name, out)
		}
	}
}

func TestTabsSwitchTheResourceKind(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod"})

	if !strings.Contains(m.View(), "IP ADDRESS") {
		t.Fatalf("VM tab should show the IP column:\n%s", m.View())
	}
	press(t, m, "right") // Templates
	if m.kind != vsphere.KindTemplate {
		t.Fatalf("kind is %q, want template", m.kind)
	}
	out := m.View()
	if !strings.Contains(out, "ubuntu-24.04-golden") {
		t.Errorf("template tab is missing the template:\n%s", out)
	}
	if strings.Contains(out, "app-01") {
		t.Errorf("template tab is still showing virtual machines:\n%s", out)
	}
	press(t, m, "left", "left") // wrap backwards to Networks
	if m.kind != vsphere.KindNetwork {
		t.Fatalf("kind is %q, want network after wrapping backwards", m.kind)
	}
}

func TestFilterNarrowsTheTable(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod"})

	press(t, m, "/", "b", "u", "i", "l", "d")
	rows := m.rows()
	if len(rows) != 1 || rows[0].name != "build-runner-3" {
		t.Fatalf("filter %q kept %d rows, want only build-runner-3", m.filter.Value(), len(rows))
	}
	press(t, m, "enter") // accept the filter, keep it applied
	if m.filtering {
		t.Error("enter should leave the filter input")
	}
	if got := len(m.rows()); got != 1 {
		t.Errorf("filter was dropped on enter: %d rows", got)
	}
	press(t, m, "esc")
	if got := len(m.rows()); got != 2 {
		t.Errorf("esc should clear the filter, got %d rows", got)
	}
}

func TestAllContextsMergesAndLabelsRows(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod"})

	press(t, m, "a")
	if !m.allScope {
		t.Fatal("'a' should widen the scope to every vCenter")
	}
	if got := len(m.rows()); got != 4 {
		t.Fatalf("all-contexts shows %d VMs, want 4 (two per vCenter)", got)
	}
	out := m.View()
	if !strings.Contains(out, "VCENTER") {
		t.Errorf("merged view must name the vCenter each row came from:\n%s", out)
	}
	press(t, m, "a")
	if got := len(m.rows()); got != 2 {
		t.Errorf("narrowing back gives %d rows, want 2", got)
	}
}

// TestOneBrokenVCenterKeepsTheRest is the behaviour the whole tool exists for,
// restated in the interface: a customer environment behind a dead proxy costs
// one line, not the screen.
func TestOneBrokenVCenterKeepsTheRest(t *testing.T) {
	b := twoHealthy()
	b.failures = map[string]error{"customer-a": errors.New("socks5 proxy 127.0.0.1:1080 unreachable: connection refused")}
	m := newTestModel(t, b, Options{Current: "prod", AllContexts: true})

	if got := len(m.rows()); got != 2 {
		t.Fatalf("healthy vCenter contributed %d rows, want 2", got)
	}
	out := m.View()
	if !strings.Contains(out, "app-01") {
		t.Errorf("healthy results were lost:\n%s", out)
	}
	if !strings.Contains(out, "socks5 proxy") {
		t.Errorf("the failure must be reported, not swallowed:\n%s", out)
	}
	if !strings.Contains(out, "1 failed") {
		t.Errorf("header should count the failure:\n%s", out)
	}
}

func TestDetailViewShowsEveryProperty(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod"})

	press(t, m, "enter")
	if m.mode != modeDetail {
		t.Fatalf("enter on a row should open the detail view, mode is %v", m.mode)
	}
	out := m.View()
	for _, want := range []string{"app-01", "Ubuntu Linux (64-bit)", "10.20.0.11", "16G", "esxi-01", "/Taipei/vm/app-01", "front end"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail view is missing %q:\n%s", want, out)
		}
	}
	press(t, m, "esc")
	if m.mode != modeBrowse {
		t.Error("esc should return to the table")
	}
}

func TestEnterOnTheSidebarSwitchesContext(t *testing.T) {
	b := twoHealthy()
	m := newTestModel(t, b, Options{Current: "prod"})

	press(t, m, "tab") // focus the sidebar
	if m.pane != paneContexts {
		t.Fatalf("tab should move focus to the sidebar, pane is %v", m.pane)
	}
	press(t, m, "up") // customer-a sorts first in the fixture order
	press(t, m, "enter")

	if m.current().cc.Name != "customer-a" {
		t.Fatalf("selected context is %q, want customer-a", m.current().cc.Name)
	}
	if b.calls["customer-a"] != 1 {
		t.Errorf("switching to a context should fetch it once, got %d", b.calls["customer-a"])
	}
	if m.pane != paneResources {
		t.Error("opening a context should hand the arrow keys back to the table")
	}
}

func TestDoctorPanelShowsTheFailingStage(t *testing.T) {
	b := twoHealthy()
	b.failures = map[string]error{"prod": errors.New("login failed")}
	b.diagnoses = map[string]*vsphere.Diagnosis{
		"prod": {
			Context:  "prod",
			Endpoint: "https://vcsa.prod.internal",
			Route:    "Direct",
			TLS:      "Pinned thumbprint",
			Checks: []vsphere.Check{
				{Name: "Configuration valid", Status: vsphere.CheckPass},
				{Name: "TCP connection", Status: vsphere.CheckPass, Detail: "12 ms"},
				{Name: "Authentication", Status: vsphere.CheckFail, Err: errors.New("incorrect user name or password")},
				{Name: "API available", Status: vsphere.CheckSkip, Detail: "skipped"},
			},
		},
	}
	m := newTestModel(t, b, Options{Current: "prod"})

	press(t, m, "d")
	if m.mode != modeDoctor {
		t.Fatalf("'d' should open the diagnosis panel, mode is %v", m.mode)
	}
	out := m.View()
	for _, want := range []string{"Diagnosing prod", "Authentication", "incorrect user name or password", "Stopped at the first failing stage"} {
		if !strings.Contains(out, want) {
			t.Errorf("diagnosis panel is missing %q:\n%s", want, out)
		}
	}
}

func TestHelpListsEveryBinding(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod"})
	press(t, m, "?")
	out := m.View()
	for _, want := range []string{"Keys", "switch pane", "all vCenters", "diagnose"} {
		if !strings.Contains(out, want) {
			t.Errorf("help is missing %q:\n%s", want, out)
		}
	}
}

// TestReloadRefetches checks 'r' against 'R': both start from a customer-a
// call count of 1, not 0, because Init already prefetched it in the
// background (Section 4) even though prod alone is in scope.
func TestReloadRefetches(t *testing.T) {
	b := twoHealthy()
	m := newTestModel(t, b, Options{Current: "prod"})
	if b.calls["customer-a"] != 1 {
		t.Fatalf("customer-a calls after Init = %d, want 1 from the background prefetch", b.calls["customer-a"])
	}
	press(t, m, "r")
	if b.calls["prod"] != 2 {
		t.Errorf("'r' should refetch the context in scope, calls: %d", b.calls["prod"])
	}
	if b.calls["customer-a"] != 1 {
		t.Errorf("'r' must not reach out of scope, customer-a calls: %d", b.calls["customer-a"])
	}
	press(t, m, "R")
	if b.calls["customer-a"] != 2 {
		t.Errorf("'R' should refetch every context, customer-a calls: %d", b.calls["customer-a"])
	}
}

// TestReloadFailureKeepsShowingTheStaleData is the stale-while-revalidate
// half of Section 4: a context that loaded once and whose next refresh fails
// must keep showing what it already had, with the failure noted rather than
// the table going blank.
func TestReloadFailureKeepsShowingTheStaleData(t *testing.T) {
	b := twoHealthy()
	m := newTestModel(t, b, Options{Current: "prod"})
	if n := len(m.rows()); n == 0 {
		t.Fatalf("prod has no rows after the initial load")
	}
	st := m.byName["prod"]
	if st.rowStatus() != statusGood {
		t.Fatalf("status after a healthy load = %v, want good", st.rowStatus())
	}

	if b.failures == nil {
		b.failures = map[string]error{}
	}
	b.failures["prod"] = errors.New("connection reset")
	press(t, m, "r")

	if n := len(m.rows()); n == 0 {
		t.Errorf("a failed refresh erased the stale rows instead of keeping them")
	}
	if st.err == nil {
		t.Error("a failed refresh left no error recorded")
	}
	if st.rowStatus() != statusWarn {
		t.Errorf("status after a stale-but-failing refresh = %v, want warn (not bad — there is still real data)", st.rowStatus())
	}
	if !m.messageBad || !strings.Contains(m.message, "refresh failed") {
		t.Errorf("message does not report the failed refresh: bad=%v %q", m.messageBad, m.message)
	}
}

// TestTabBarFlagsAKindWithAListingError checks that a resource kind
// ListInventory could not enumerate (Inventory.Errors) is visible from the
// tab bar, not just discoverable by opening that tab and finding it empty.
func TestTabBarFlagsAKindWithAListingError(t *testing.T) {
	b := twoHealthy()
	inv := *b.inventories["prod"]
	inv.Datastores = nil
	inv.Errors = []vsphere.InventoryError{{Kind: vsphere.KindDatastore, Message: "permission denied"}}
	b.inventories["prod"] = &inv

	m := newTestModel(t, b, Options{Current: "prod"})
	out := m.View()
	if !strings.Contains(out, "Datastores") {
		t.Fatalf("tab bar is missing the Datastores tab:\n%s", out)
	}
	if !m.kindErrorInScope(vsphere.KindDatastore) {
		t.Error("kindErrorInScope(KindDatastore) = false, want true")
	}
	if m.kindErrorInScope(vsphere.KindHost) {
		t.Error("kindErrorInScope(KindHost) = true, want false — hosts were never denied")
	}
}

// TestNarrowTerminalKeepsTheNameColumn checks the column layout degrades by
// dropping detail from the right rather than by squeezing names into nothing.
func TestNarrowTerminalKeepsTheNameColumn(t *testing.T) {
	cols := columnsFor(vsphere.KindVM, false)
	widths := layoutColumns(cols, 40-glyphGutter)

	if widths[0] < minNameWidth {
		t.Errorf("name column is %d wide, want at least %d", widths[0], minNameWidth)
	}
	total, drawn := 0, 0
	for i, w := range widths {
		if w == 0 {
			continue
		}
		drawn++
		total += w
		if i > 0 && widths[i-1] == 0 {
			t.Errorf("column %q is drawn after a dropped column; columns must drop from the right", cols[i].title)
		}
	}
	if drawn == len(cols) {
		t.Error("a 40 column terminal should have dropped something")
	}
	if width := glyphGutter + total + cellGap*(drawn-1); width > 40 {
		t.Errorf("laid out %d columns of total width %d in 40 columns", drawn, width)
	}
}

func TestRowsAreFlattenedForEveryKind(t *testing.T) {
	inv := inventoryFor("prod")
	for _, kind := range vsphere.AllKinds {
		rows := rowsFor(inv, kind, true)
		if len(rows) == 0 {
			t.Errorf("%s produced no rows", kind)
			continue
		}
		cols := columnsFor(kind, true)
		for _, r := range rows {
			if len(r.cells) != len(cols) {
				t.Errorf("%s row %q has %d cells for %d columns", kind, r.name, len(r.cells), len(cols))
			}
			if len(r.detail) == 0 {
				t.Errorf("%s row %q has no detail fields", kind, r.name)
			}
		}
	}
}

// TestSetupFormOpensWithNoContexts is the entry-experience requirement from
// the v0.1.0 plan: a fresh install has nowhere to browse, so the interface
// opens straight into adding the first context instead of an empty table.
func TestSetupFormOpensWithNoContexts(t *testing.T) {
	m := newTestModel(t, &fakeBackend{}, Options{})
	if m.mode != modeForm {
		t.Fatalf("mode is %v, want modeForm", m.mode)
	}
	out := m.View()
	if !strings.Contains(out, "Add a vCenter") || !strings.Contains(out, "first one") {
		t.Errorf("setup form did not open as expected:\n%s", out)
	}
}

// fillNewContextBasics types name, endpoint and username and leaves the
// cursor on the Credential row, which is where every new-context test above
// diverges (prompt vs. keyring, direct vs. socks5, and so on).
func fillNewContextBasics(t *testing.T, m *Model, name, endpoint, username string) {
	t.Helper()
	typeText(t, m, name)
	press(t, m, "down")
	typeText(t, m, endpoint)
	press(t, m, "down")
	typeText(t, m, username)
	press(t, m, "down") // -> Credential
}

func TestNewContextFormSavesAndSelectsIt(t *testing.T) {
	b := twoHealthy()
	m := newTestModel(t, b, Options{Current: "prod"})

	press(t, m, "n")
	settleForm(m)
	if m.mode != modeForm {
		t.Fatalf("'n' should open the form, mode is %v", m.mode)
	}
	if m.form.editing {
		t.Fatal("'n' should open a blank form, not an edit")
	}

	fillNewContextBasics(t, m, "staging", "https://vcsa.staging.internal", "operator@vsphere.local")
	press(t, m, "right")                                // Credential: keyring -> prompt, skips the password field
	press(t, m, "down", "down", "down", "down", "down") // Route, TLS, Datacenter, Current, Test
	press(t, m, "down")                                 // Save
	press(t, m, "enter")

	if m.mode != modeBrowse {
		t.Fatalf("save should return to browse, mode is %v", m.mode)
	}
	if len(b.contexts) != 3 {
		t.Fatalf("backend has %d contexts, want 3", len(b.contexts))
	}
	cur := m.current()
	if cur == nil || cur.cc.Name != "staging" {
		t.Fatalf("selected context is %v, want staging", cur)
	}
	if b.calls["staging"] == 0 {
		t.Error("the new context should have been loaded right after saving")
	}
}

func TestFormBlocksSaveOnFailedTestUntilSaveAnyway(t *testing.T) {
	b := twoHealthy()
	b.testDiag = map[string]*vsphere.Diagnosis{
		"broken": {
			Context: "broken",
			Checks:  []vsphere.Check{{Name: "TCP connection", Status: vsphere.CheckFail, Err: errors.New("connection refused")}},
		},
	}
	m := newTestModel(t, b, Options{Current: "prod"})

	press(t, m, "n")
	settleForm(m)
	fillNewContextBasics(t, m, "broken", "https://vcsa.broken.internal", "operator@vsphere.local")
	press(t, m, "right")                                // prompt credential
	press(t, m, "down", "down", "down", "down", "down") // Route, TLS, Datacenter, Current, Test
	press(t, m, "enter")                                // run the test — fails

	if m.mode != modeForm {
		t.Fatalf("a failed test must not close the form, mode is %v", m.mode)
	}
	if !strings.Contains(m.View(), "connection refused") {
		t.Errorf("form should show the failing diagnosis:\n%s", m.View())
	}

	press(t, m, "down") // -> Save
	press(t, m, "enter")
	if m.mode != modeForm {
		t.Fatalf("save should be blocked by the failing test, mode is %v", m.mode)
	}
	if len(b.contexts) != 2 {
		t.Fatalf("nothing should be saved yet, have %d contexts", len(b.contexts))
	}
	if !m.form.forceSave {
		t.Fatal("a failed save should flip the Save row to \"Save anyway\"")
	}

	press(t, m, "enter") // same row, now "Save anyway"
	if m.mode != modeBrowse {
		t.Fatalf("save anyway should go through, mode is %v", m.mode)
	}
	if len(b.contexts) != 3 {
		t.Errorf("context should be saved despite the failing test, have %d", len(b.contexts))
	}
}

func TestEditContextPrefillsAndUpdatesInPlace(t *testing.T) {
	b := twoHealthy()
	m := newTestModel(t, b, Options{Current: "prod"})

	press(t, m, "e")
	settleForm(m)
	if m.mode != modeForm {
		t.Fatalf("'e' should open the form, mode is %v", m.mode)
	}
	if !m.form.editing || m.form.origName != "prod" {
		t.Fatalf("form is not editing prod: editing=%v origName=%q", m.form.editing, m.form.origName)
	}
	if got := m.form.endpoint.Value(); got != "https://vcsa.prod.internal" {
		t.Errorf("endpoint not prefilled: %q", got)
	}

	// Row order while editing: Name(static) Endpoint Username Credential
	// Password Route TLS Datacenter Current Test Save Cancel.
	for range 7 {
		press(t, m, "down") // -> Datacenter
	}
	typeText(t, m, "Lab-DC")
	for range 3 {
		press(t, m, "down") // Current, Test, Save
	}
	press(t, m, "enter")

	if m.mode != modeBrowse {
		t.Fatalf("save should return to browse, mode is %v", m.mode)
	}
	if len(b.contexts) != 2 {
		t.Fatalf("editing should replace, not add: have %d contexts", len(b.contexts))
	}
	edited, err := findContext(b.contexts, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if edited.Datacenter != "Lab-DC" {
		t.Errorf("datacenter is %q, want Lab-DC", edited.Datacenter)
	}
	if edited.Endpoint != "https://vcsa.prod.internal" {
		t.Errorf("editing changed the endpoint to %q", edited.Endpoint)
	}
}

func findContext(contexts []*config.Context, name string) (*config.Context, error) {
	for _, cc := range contexts {
		if cc.Name == name {
			return cc, nil
		}
	}
	return nil, fmt.Errorf("context %q not found among %d", name, len(contexts))
}

func TestDeleteContextConfirmationRemovesIt(t *testing.T) {
	b := twoHealthy()
	m := newTestModel(t, b, Options{Current: "prod"})

	press(t, m, "x")
	if m.mode != modeConfirmDelete {
		t.Fatalf("'x' should open the delete confirmation, mode is %v", m.mode)
	}
	if !strings.Contains(m.View(), "Delete prod?") {
		t.Errorf("confirmation should name the context:\n%s", m.View())
	}

	press(t, m, "y")
	if m.mode != modeBrowse {
		t.Fatalf("confirming should return to browse, mode is %v", m.mode)
	}
	if len(b.contexts) != 1 || b.contexts[0].Name != "customer-a" {
		t.Fatalf("prod should have been removed, contexts: %v", b.contexts)
	}
}

func TestDeleteConfirmationCancels(t *testing.T) {
	b := twoHealthy()
	m := newTestModel(t, b, Options{Current: "prod"})

	press(t, m, "x", "n")
	if m.mode != modeBrowse {
		t.Fatalf("'n' should cancel back to browse, mode is %v", m.mode)
	}
	if len(b.contexts) != 2 {
		t.Errorf("nothing should have been removed, have %d contexts", len(b.contexts))
	}
}

// TestDeleteLastContextReopensSetup is the same principle as the empty-config
// start-up: a screen with nothing to show and no way back in is never the
// resting state.
func TestDeleteLastContextReopensSetup(t *testing.T) {
	b := &fakeBackend{contexts: []*config.Context{ctx("only", "https://vcsa.only.internal")}}
	m := newTestModel(t, b, Options{Current: "only"})

	press(t, m, "x", "y")
	if m.mode != modeForm {
		t.Fatalf("deleting the last context should reopen setup, mode is %v", m.mode)
	}
	if len(b.contexts) != 0 {
		t.Errorf("context should have been removed, have %d", len(b.contexts))
	}
}

func TestDiscoverThumbprintFillsTheField(t *testing.T) {
	b := twoHealthy()
	b.discoverThumb = "11:22:33:44"
	b.discoverSubj = "vcsa.staging.internal"
	m := newTestModel(t, b, Options{Current: "prod"})

	press(t, m, "n")
	settleForm(m)
	fillNewContextBasics(t, m, "staging", "https://vcsa.staging.internal", "operator@vsphere.local")
	press(t, m, "down", "down", "down") // Password, Route, TLS
	press(t, m, "right")                // TLS: system -> thumbprint
	press(t, m, "down", "down")         // Thumbprint, Discover
	press(t, m, "enter")

	if got := m.form.thumbprint.Value(); got != "11:22:33:44" {
		t.Fatalf("thumbprint field is %q, want the discovered value", got)
	}
	if !strings.Contains(m.View(), "Discovered vcsa.staging.internal") {
		t.Errorf("form should report what was discovered:\n%s", m.View())
	}
}

func TestFormEscapeCancelsWithoutQuitting(t *testing.T) {
	b := twoHealthy()
	m := newTestModel(t, b, Options{Current: "prod"})

	press(t, m, "n")
	settleForm(m)
	typeText(t, m, "this contains the letters q and colon: q")
	press(t, m, "esc")

	if m.mode != modeBrowse {
		t.Fatalf("esc should cancel the form, mode is %v", m.mode)
	}
	if m.quitting {
		t.Error("typing 'q' into a field must not quit the program")
	}
	if len(b.contexts) != 2 {
		t.Errorf("cancelling must not save anything, have %d contexts", len(b.contexts))
	}
}

func TestSortBringsTroubleToTheTop(t *testing.T) {
	inv := inventoryFor("prod")
	// app-01 sorts first alphabetically. Giving build-runner-3 the worse
	// status is what makes name order and status order actually disagree,
	// so the test tells them apart rather than passing by coincidence.
	inv.VMs[1].PowerState = "suspended"
	b := &fakeBackend{
		contexts:    []*config.Context{ctx("prod", "https://vcsa.prod.internal")},
		inventories: map[string]*vsphere.Inventory{"prod": inv},
	}
	m := newTestModel(t, b, Options{Current: "prod"})

	rows := m.rows()
	if rows[0].name != "app-01" {
		t.Fatalf("name order should put app-01 first, got %q", rows[0].name)
	}

	press(t, m, "s")
	if m.sortMode != sortByStatus {
		t.Fatalf("'s' should switch to status order, sortMode is %v", m.sortMode)
	}
	rows = m.rows()
	if rows[0].name != "build-runner-3" {
		t.Errorf("status order should put the suspended VM first, got %q", rows[0].name)
	}
	if !strings.Contains(m.View(), "sort: status") {
		t.Errorf("the active sort should be visible in the tab bar:\n%s", m.View())
	}

	press(t, m, "s")
	if m.sortMode != sortByName {
		t.Fatalf("'s' should cycle back to name order, sortMode is %v", m.sortMode)
	}
}

func TestSnapshotReportsCurrentPosition(t *testing.T) {
	b := twoHealthy()
	m := newTestModel(t, b, Options{Current: "prod"})

	press(t, m, "right") // Templates tab
	press(t, m, "s")     // status sort

	snap := m.Snapshot()
	if snap.Context != "prod" {
		t.Errorf("snapshot context is %q, want prod", snap.Context)
	}
	if snap.Kind != "template" {
		t.Errorf("snapshot kind is %q, want template", snap.Kind)
	}
	if snap.Sort != "status" {
		t.Errorf("snapshot sort is %q, want status", snap.Sort)
	}
}

func TestOptionsSeedTheStartingPosition(t *testing.T) {
	b := twoHealthy()
	m := New(context.Background(), b, Options{Current: "customer-a", Kind: "host", Sort: "status"})

	if got := m.current(); got == nil || got.cc.Name != "customer-a" {
		t.Errorf("starting context is %v, want customer-a", got)
	}
	if m.kind != vsphere.KindHost {
		t.Errorf("starting kind is %v, want host", m.kind)
	}
	if m.sortMode != sortByStatus {
		t.Errorf("starting sort is %v, want status", m.sortMode)
	}
}

func hasLabel(rows []formRow, label string) bool {
	for _, r := range rows {
		if r.label == label {
			return true
		}
	}
	return false
}

// TestFormRouteRowsMatchEachProxyType checks the row generation directly,
// independent of the keypresses needed to reach each route: direct shows no
// proxy fields, every proxy type shows an address, only socks5 shows the
// remote-DNS toggle (http and https always resolve at the proxy, so there is
// nothing to choose), and the password field only appears once a proxy
// username has actually been typed.
func TestFormRouteRowsMatchEachProxyType(t *testing.T) {
	f := newContextForm(nil)

	if hasLabel(f.rows(), "Proxy address") {
		t.Error("direct route should not show proxy fields")
	}

	f.transportIdx = 1 // socks5
	if rows := f.rows(); !hasLabel(rows, "Proxy address") || !hasLabel(rows, "Resolve DNS at the proxy") {
		t.Error("socks5 should show a proxy address and the remote-DNS toggle")
	}

	f.transportIdx = 2 // http
	if rows := f.rows(); !hasLabel(rows, "Proxy address") {
		t.Error("http should show a proxy address field")
	} else if hasLabel(rows, "Resolve DNS at the proxy") {
		t.Error("http has no remote-DNS choice — it always resolves at the proxy")
	}

	f.transportIdx = 3 // https
	if rows := f.rows(); !hasLabel(rows, "Proxy address") {
		t.Error("https should show a proxy address field")
	} else if hasLabel(rows, "Resolve DNS at the proxy") {
		t.Error("https has no remote-DNS choice either")
	} else if hasLabel(rows, "Proxy password") {
		t.Error("the password field should not appear before a proxy username is typed")
	}

	f.proxyUser.SetValue("svc-proxy")
	if !hasLabel(f.rows(), "Proxy password") {
		t.Error("a non-empty proxy username should reveal the password field")
	}
}

func TestNewContextFormSavesAnHTTPSProxyRoute(t *testing.T) {
	b := twoHealthy()
	m := newTestModel(t, b, Options{Current: "prod"})

	press(t, m, "n")
	settleForm(m)
	fillNewContextBasics(t, m, "secure-proxy", "https://vcsa.secure.internal", "operator@vsphere.local")
	press(t, m, "down")                    // -> Password (leave blank)
	press(t, m, "down")                    // -> Route
	press(t, m, "right", "right", "right") // direct -> socks5 -> http -> https
	if m.form.transportIdx != 3 {
		t.Fatalf("transportIdx is %d, want 3 (https)", m.form.transportIdx)
	}
	press(t, m, "down") // -> Proxy address
	typeText(t, m, "proxy.example.internal:3128")
	press(t, m, "down") // -> Proxy username
	typeText(t, m, "svc-proxy")
	press(t, m, "down") // -> Proxy password (now visible)
	typeText(t, m, "s3cret")
	press(t, m, "down", "down", "down", "down", "down") // TLS, Datacenter, Current, Test, Save
	press(t, m, "enter")

	if m.mode != modeBrowse {
		t.Fatalf("save should return to browse, mode is %v", m.mode)
	}
	saved, err := findContext(b.contexts, "secure-proxy")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Transport.Type != config.TransportHTTPSProxy {
		t.Errorf("route type is %q, want https", saved.Transport.Type)
	}
	if saved.Transport.Address != "proxy.example.internal:3128" {
		t.Errorf("proxy address is %q", saved.Transport.Address)
	}
	if saved.Transport.Username != "svc-proxy" {
		t.Errorf("proxy username is %q, want svc-proxy", saved.Transport.Username)
	}
}

func TestEditContextPrefillsAnHTTPProxyRoute(t *testing.T) {
	b := &fakeBackend{contexts: []*config.Context{func() *config.Context {
		cc := ctx("via-proxy", "https://vcsa.via-proxy.internal")
		cc.Transport = config.TransportConfig{Type: config.TransportHTTPProxy, Address: "10.0.0.1:8080", Username: "svc"}
		return cc
	}()}}
	m := newTestModel(t, b, Options{Current: "via-proxy"})

	press(t, m, "e")
	settleForm(m)
	if m.form.transportIdx != 2 {
		t.Fatalf("transportIdx is %d, want 2 (http)", m.form.transportIdx)
	}
	if got := m.form.proxyAddr.Value(); got != "10.0.0.1:8080" {
		t.Errorf("proxy address not prefilled: %q", got)
	}
	if got := m.form.proxyUser.Value(); got != "svc" {
		t.Errorf("proxy username not prefilled: %q", got)
	}
	if hasLabel(m.form.rows(), "Resolve DNS at the proxy") {
		t.Error("http should not show the remote-DNS toggle even when editing")
	}
}
