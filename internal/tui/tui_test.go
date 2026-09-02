package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vc-tui/internal/config"
	"github.com/easonliuuuuu/vc-tui/internal/session"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

// fakeBackend answers instantly from fixtures. The whole point of the Backend
// interface is that everything below is exercised here without a vCenter, a
// proxy or a certificate anywhere in sight.
type fakeBackend struct {
	contexts    []*config.Context
	inventories map[string]*vsphere.Inventory
	failures    map[string]error
	diagnoses   map[string]*vsphere.Diagnosis
	calls       map[string]int
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
	return b.inventories[cc.Name], nil
}

func (b *fakeBackend) Status(name string) (session.Status, bool) {
	return session.Status{Name: name}, false
}

func (b *fakeBackend) Diagnose(_ context.Context, cc *config.Context) *vsphere.Diagnosis {
	return b.diagnoses[cc.Name]
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
	return m
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

func twoHealthy() *fakeBackend {
	return &fakeBackend{
		contexts: []*config.Context{ctx("customer-a", "https://vcsa.customer-a.internal"), ctx("prod", "https://vcsa.prod.internal")},
		inventories: map[string]*vsphere.Inventory{
			"customer-a": inventoryFor("customer-a"),
			"prod":       inventoryFor("prod"),
		},
	}
}

func TestBrowseShowsOnlyTheSelectedContext(t *testing.T) {
	b := twoHealthy()
	m := newTestModel(t, b, Options{Current: "prod"})

	if got := b.calls["prod"]; got != 1 {
		t.Fatalf("prod fetched %d times, want 1", got)
	}
	if got := b.calls["customer-a"]; got != 0 {
		t.Errorf("customer-a was fetched %d times; a single-context scope must not touch the others", got)
	}
	out := m.View()
	if !strings.Contains(out, "app-01") {
		t.Errorf("VM list is missing app-01:\n%s", out)
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

func TestReloadRefetches(t *testing.T) {
	b := twoHealthy()
	m := newTestModel(t, b, Options{Current: "prod"})
	press(t, m, "r")
	if b.calls["prod"] != 2 {
		t.Errorf("'r' should refetch the context in scope, calls: %d", b.calls["prod"])
	}
	if b.calls["customer-a"] != 0 {
		t.Errorf("'r' must not reach out of scope, customer-a calls: %d", b.calls["customer-a"])
	}
	press(t, m, "R")
	if b.calls["customer-a"] != 1 {
		t.Errorf("'R' should refetch every context, customer-a calls: %d", b.calls["customer-a"])
	}
}

// TestNarrowTerminalKeepsTheNameColumn checks the column layout degrades by
// dropping detail from the right rather than by squeezing names into nothing.
func TestNarrowTerminalKeepsTheNameColumn(t *testing.T) {
	cols := columnsFor(vsphere.KindVM, false)
	widths := layoutColumns(cols, 40)

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
	if width := total + cellGap*(drawn-1); width > 40 {
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
