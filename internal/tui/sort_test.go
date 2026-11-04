package tui

import (
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// sortFixture is one vCenter whose objects disagree on every sortable
// column, so each order below lands on a different arrangement. Names that
// differ only in a number (web-2, web-10) and values left unreported (no
// memory, no IP, no version) are the cases a plain string sort gets wrong.
func sortFixture(t *testing.T) *Model {
	t.Helper()
	inv := inventoryFor("prod")
	loc := func(name string) vsphere.Location {
		return vsphere.Location{Context: "prod", Datacenter: "Taipei", Path: "/Taipei/vm/" + name}
	}
	inv.VMs = []vsphere.VM{
		{Location: loc("web-10"), ID: "vm-1", Name: "web-10", PowerState: "poweredOn", CPU: 2, MemoryMB: 8192, IPAddress: "10.0.0.10", Host: "esxi-02"},
		{Location: loc("web-2"), ID: "vm-2", Name: "web-2", PowerState: "poweredOff", CPU: 8, MemoryMB: 4096, IPAddress: "10.0.0.9", Host: "esxi-01"},
		{Location: loc("db-1"), ID: "vm-3", Name: "db-1", PowerState: "suspended", CPU: 4, Host: "esxi-03"},
		{Location: loc("app-1"), ID: "vm-4", Name: "app-1", PowerState: "poweredOn", CPU: 4, MemoryMB: 8192},
	}
	inv.Templates = []vsphere.VM{
		{Location: loc("tpl-small"), ID: "tpl-1", Name: "tpl-small", IsTemplate: true, CPU: 1, MemoryMB: 2048, StorageGB: 20, GuestOS: "Windows Server 2022"},
		{Location: loc("tpl-big"), ID: "tpl-2", Name: "tpl-big", IsTemplate: true, CPU: 8, MemoryMB: 16384, StorageGB: 200, GuestOS: "Debian 12"},
	}
	host := func(name, version string, cpuUsed, memUsed int64, vms int) vsphere.Host {
		return vsphere.Host{
			Location: loc(name), ID: name, Name: name, ConnectionState: "connected", Version: version,
			CPUCores: 10, CPUMHz: 1000, CPUUsageMHz: cpuUsed, MemoryMB: 1000, MemoryUsageMB: memUsed, VMCount: vms,
		}
	}
	inv.Hosts = []vsphere.Host{
		host("esxi-01", "8.0.10", 2000, 900, 3),
		host("esxi-02", "8.0.3", 9000, 100, 12),
		host("esxi-03", "", 0, 0, 0),
	}
	inv.Hosts[2].CPUCores, inv.Hosts[2].MemoryMB = 0, 0
	inv.Clusters = []vsphere.Cluster{
		{Location: loc("small"), ID: "cl-1", Name: "small", Hosts: 2, CPUCores: 16, TotalMemoryMB: 1024},
		{Location: loc("large"), ID: "cl-2", Name: "large", Hosts: 8, CPUCores: 256, TotalMemoryMB: 8192, DRSEnabled: true},
	}
	ds := func(name string, capacity, free int64) vsphere.Datastore {
		return vsphere.Datastore{Location: loc(name), ID: name, Name: name, Type: "VMFS", Accessible: true, CapacityBytes: capacity, FreeBytes: free}
	}
	inv.Datastores = []vsphere.Datastore{
		ds("ds-half", 100<<30, 50<<30),
		ds("ds-full", 100<<30, 5<<30),
		ds("ds-big", 1000<<30, 900<<30),
	}
	b := &fakeBackend{
		contexts:    []*config.Context{ctx("prod", "https://vcsa.prod.internal")},
		inventories: map[string]*vsphere.Inventory{"prod": inv},
	}
	m := newTestModel(t, b, Options{Current: "prod"})
	m.height = 40
	return m
}

// TestSortMenuOrdersEachTab drives the sort menu the way an operator does —
// a tab's number, "s", a column's letter, perhaps "S" to reverse — and reads
// the table back.
func TestSortMenuOrdersEachTab(t *testing.T) {
	tests := []struct {
		name   string
		keys   []string
		want   string
		header string
	}{
		{"names compare their numbers", nil, "app-1,db-1,web-2,web-10", "sort: name"},
		{"name reversed", []string{"S"}, "web-10,web-2,db-1,app-1", "sort: name ↓"},
		{"memory starts largest first, unreported last", []string{"s", "m"}, "app-1,web-10,web-2,db-1", "sort: memory ↓"},
		{"memory reversed keeps unreported last", []string{"s", "m", "S"}, "web-2,app-1,web-10,db-1", "sort: memory ↑"},
		{"choosing the current order reverses it", []string{"s", "m", "s", "m"}, "web-2,app-1,web-10,db-1", "sort: memory ↑"},
		{"r in the menu reverses the chosen column", []string{"s", "down", "down", "r"}, "web-10,app-1,db-1,web-2", "sort: CPU ↑"},
		{"IP addresses compare as numbers", []string{"s", "i"}, "web-2,web-10,app-1,db-1", "sort: IP address ↑"},
		{"power puts trouble first", []string{"s", "p"}, "db-1,app-1,web-10,web-2", "sort: power ↑"},
		{"host", []string{"s", "h"}, "web-2,web-10,db-1,app-1", "sort: host ↑"},
		{"esc leaves the order alone", []string{"s", "m", "s", "esc"}, "app-1,web-10,web-2,db-1", "sort: memory ↓"},
		{"template disk", []string{"2", "s", "i"}, "tpl-big,tpl-small", "sort: disk ↓"},
		{"template guest OS", []string{"2", "s", "g"}, "tpl-big,tpl-small", "sort: guest OS ↑"},
		{"host CPU used share", []string{"3", "s", "c"}, "esxi-02,esxi-01,esxi-03", "sort: CPU used ↓"},
		{"host memory used share", []string{"3", "s", "m"}, "esxi-01,esxi-02,esxi-03", "sort: memory used ↓"},
		{"host versions compare their numbers", []string{"3", "s", "e"}, "esxi-02,esxi-01,esxi-03", "sort: version ↑"},
		{"host VM count", []string{"3", "s", "v"}, "esxi-02,esxi-01,esxi-03", "sort: VMs ↓"},
		{"cluster cores", []string{"4", "s", "c"}, "large,small", "sort: cores ↓"},
		{"cluster DRS on first", []string{"4", "s", "x"}, "large,small", "sort: DRS ↓"},
		{"datastore used share", []string{"5", "s", "u"}, "ds-full,ds-half,ds-big", "sort: used % ↓"},
		{"datastore free space", []string{"5", "s", "f"}, "ds-big,ds-half,ds-full", "sort: free ↓"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := sortFixture(t)
			press(t, m, tt.keys...)
			if m.sortMenu != nil {
				t.Fatal("the menu should close once an order is chosen or cancelled")
			}
			if got := strings.Join(rowNames(m.rows()), ","); got != tt.want {
				t.Errorf("rows = %s, want %s", got, tt.want)
			}
			if got := screenLine(t, m, "vsfleet"); !strings.HasSuffix(strings.TrimSpace(got), tt.header) {
				t.Errorf("header %q should end with %q", got, tt.header)
			}
		})
	}
}

// TestSortMenuShowsWhereItIs checks what the open menu and the table say:
// the menu lists the tab's orders with its cursor on the current one, the
// key line names only the menu's keys, and the sorted column is marked —
// including CPU, whose heading fills its column, so the arrow takes the gap.
func TestSortMenuShowsWhereItIs(t *testing.T) {
	m := sortFixture(t)
	press(t, m, "s", "m", "s")

	if m.sortMenu == nil {
		t.Fatal("s should open the sort menu")
	}
	if got := screenLine(t, m, "▸ m  memory"); !strings.Contains(got, "↓") {
		t.Errorf("the cursor should start on the current order, marked with its direction: %q", got)
	}
	for _, label := range []string{"Sort VMs", "n  name", "p  power", "c  CPU", "i  IP address", "h  host", "d  datacenter"} {
		screenLine(t, m, label)
	}
	requireFooter(t, m, []string{"enter sort", "r reverse", "esc cancel"}, []string{"H history", "/ filter"})

	press(t, m, "esc")
	if got := screenLine(t, m, "NAME"); !strings.Contains(got, "↓MEM") {
		t.Errorf("the MEM heading should carry the direction: %q", got)
	}
	press(t, m, "s", "c")
	if got := screenLine(t, m, "NAME"); !strings.Contains(got, "↓CPU") || !strings.Contains(got, "POWER ↓CPU") {
		t.Errorf("a full-width heading should take its arrow from the gap: %q", got)
	}
}

// TestRememberedSortsCarryOver covers the two forms a state file holds: the
// per-tab orders this release writes, and the single "status" older ones
// wrote for every tab.
func TestRememberedSortsCarryOver(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want map[vsphere.Kind]sortOrder
	}{
		{
			name: "per-tab orders",
			opts: Options{Sorts: map[string]string{"vm": "memory:asc", "datastore": "used:desc", "host": "no-such-column:desc", "nonsense": "name:desc"}},
			want: map[vsphere.Kind]sortOrder{vsphere.KindVM: {key: "memory"}, vsphere.KindDatastore: {key: "used", desc: true}},
		},
		{
			name: "an older release's status order",
			opts: Options{Sort: "status"},
			want: map[vsphere.Kind]sortOrder{
				vsphere.KindVM:        {key: "power"},
				vsphere.KindHost:      {key: "status"},
				vsphere.KindCluster:   {key: "status"},
				vsphere.KindVApp:      {key: "status"},
				vsphere.KindDatastore: {key: "status"},
				vsphere.KindNetwork:   {key: "status"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.Current = "prod"
			m := newTestModel(t, twoHealthy(), tt.opts)
			for _, kind := range vsphere.AllKinds {
				if got, want := m.sortFor(kind), tt.want[kind]; got != want {
					t.Errorf("%s starts in %+v, want %+v", kind, got, want)
				}
			}
			snap := m.Snapshot()
			for kind, o := range tt.want {
				if _, ok := snap.Sorts[string(kind)]; !ok {
					t.Errorf("snapshot drops %s's %+v: %v", kind, o, snap.Sorts)
				}
			}
		})
	}
}
