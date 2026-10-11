package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// filedModel opens prod's VMs tab on an inventory filed into folders the way
// the vSphere client shows them: jump-01 at the top of Taipei, web-01 and
// web-02 in /Prod/Customer-Facing-Web, db-01 in /Prod/DB, and api-01 in the
// vApp web-stack under /Apps with cache-01 in its nested vApp web-cache. Its one template is
// in /Templates/Linux. dcs adds one VM filed at the top of each further
// datacenter.
func filedModel(t *testing.T, dcs ...string) *Model {
	t.Helper()
	b := twoHealthy()
	inv := b.inventories["prod"]
	vm := func(dc, name, folder, host string) vsphere.VM {
		return vsphere.VM{
			Location: vsphere.Location{Context: "prod", Datacenter: dc, Path: "/" + dc + "/vm" + strings.TrimSuffix(folder, "/") + "/" + name},
			ID:       "prod-" + name, Name: name, PowerState: "poweredOn", CPU: 2, MemoryMB: 4096,
			Host: host, Folder: folder,
		}
	}
	inv.VMs = []vsphere.VM{
		vm("Taipei", "api-01", "/Apps/web-stack", "esxi-02"),
		vm("Taipei", "cache-01", "/Apps/web-stack/web-cache", "esxi-01"),
		vm("Taipei", "db-01", "/Prod/DB", "esxi-02"),
		vm("Taipei", "jump-01", "/", "esxi-01"),
		vm("Taipei", "web-01", "/Prod/Customer-Facing-Web", "esxi-01"),
		vm("Taipei", "web-02", "/Prod/Customer-Facing-Web", "esxi-02"),
	}
	inv.Templates[0].Folder = "/Templates/Linux"
	for _, dc := range dcs {
		inv.VMs = append(inv.VMs, vm(dc, "dr-"+strings.ToLower(dc), "/", "esxi-01"))
	}
	m := newTestModel(t, b, Options{Current: "prod"})
	m.height = 30
	return m
}

func selected(t *testing.T, m *Model) string {
	t.Helper()
	r, ok := m.currentRow()
	if !ok {
		t.Fatal("no row is selected")
	}
	return r.name
}

func TestFolderViewBrowsesOneLevelAtATime(t *testing.T) {
	m := filedModel(t, "Tainan")
	if got := selected(t, m); got != "api-01" {
		t.Fatalf("the flat list starts on %q, want api-01", got)
	}

	steps := []struct {
		keys     []string
		rows     string
		selected string
		where    string
	}{
		// t opens the folder of the VM under the cursor and keeps it selected.
		{[]string{"t"}, "web-cache,api-01", "api-01", "in Taipei › Apps › web-stack"},
		{[]string{"esc"}, "web-stack", "web-stack", "in Taipei › Apps"},
		{[]string{"esc"}, "Apps,Prod,jump-01", "Apps", "in Taipei  ·"},
		// Two datacenters in scope: the top level lists them.
		{[]string{"esc"}, "Tainan,Taipei", "Taipei", "in all datacenters"},
		{[]string{"esc"}, "Tainan,Taipei", "Taipei", "in all datacenters"},
		{[]string{"enter", "j", "enter"}, "Customer-Facing-Web,DB", "Customer-Facing-Web", "in Taipei › Prod"},
		{[]string{"enter"}, "web-01,web-02", "web-01", "in Taipei › Prod › Customer-Facing-Web"},
		// t lists every VM again and keeps the selection.
		{[]string{"t"}, "api-01,cache-01,db-01,jump-01,web-01,web-02,dr-tainan", "web-01", ""},
	}
	for _, s := range steps {
		press(t, m, s.keys...)
		if got := strings.Join(rowNames(m.rows()), ","); got != s.rows {
			t.Fatalf("after %v rows = %s, want %s", s.keys, got, s.rows)
		}
		if got := selected(t, m); got != s.selected {
			t.Errorf("after %v the cursor is on %q, want %q", s.keys, got, s.selected)
		}
		if msg := ansi.Strip(m.viewMessage()); !strings.Contains(msg, s.where) {
			t.Errorf("after %v the message line is %q, want %q", s.keys, msg, s.where)
		}
	}

	// At 80 columns NAME is narrower than the label; a folder row runs on
	// over the columns it leaves empty instead of cutting its count short.
	press(t, m, "t", "esc")
	m.width = 80
	if out := ansi.Strip(m.View()); !strings.Contains(out, "▹ Customer-Facing-Web/  2 VMs") {
		t.Errorf("a folder row names its folder and what it holds:\n%s", out)
	}
}

func TestFolderViewNarrowsToMatchesFlat(t *testing.T) {
	tests := []struct {
		name string
		do   func(*Model)
		want string
		hint string
	}{
		{
			name: "a filter searches the open folder and below",
			do: func(m *Model) {
				press(t, m, "/")
				typeText(t, m, "0")
			},
			want: "db-01,web-01,web-02",
			hint: "3 VMs in Taipei › Prod",
		},
		{
			name: "a jump lists its matches wherever they are filed",
			do: func(m *Model) {
				m.jump = &jumpConstraint{kind: vsphere.KindVM, matcher: "host", value: "esxi-01", label: "VMs on esxi-01"}
			},
			want: "cache-01,jump-01,web-01",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := filedModel(t)
			// One datacenter: t on api-01 and two steps up land on its top
			// folder, with no level that lists a single datacenter.
			press(t, m, "t", "esc", "esc", "esc")
			if got := strings.Join(rowNames(m.rows()), ","); got != "Apps,Prod,jump-01" {
				t.Fatalf("top folder rows = %s", got)
			}
			press(t, m, "j", "enter")
			tc.do(m)
			if got := strings.Join(rowNames(m.rows()), ","); got != tc.want {
				t.Errorf("rows = %s, want %s", got, tc.want)
			}
			if tc.hint != "" && !strings.Contains(m.filterHint(), tc.hint) {
				t.Errorf("filter hint = %q, want %q", m.filterHint(), tc.hint)
			}
		})
	}
}

func TestDetailPaneStepsOverFolders(t *testing.T) {
	m := filedModel(t)
	press(t, m, "t", "esc", "esc", "G", "enter")
	if m.mode != modeDetail || selected(t, m) != "jump-01" {
		t.Fatalf("mode %v on %q, want jump-01's detail", m.mode, selected(t, m))
	}
	press(t, m, "left")
	if got := selected(t, m); got != "jump-01" || m.mode != modeDetail {
		t.Errorf("← from the only VM on the level moved to %q; folders have no pane", got)
	}
}

func TestSearchResultOpensInItsFolder(t *testing.T) {
	m := filedModel(t)
	press(t, m, "t", "tab")
	typeText(t, m, "db-01")
	press(t, m, "enter", "enter")
	if m.mode != modeDetail || selected(t, m) != "db-01" {
		t.Fatalf("mode %v on %q, want db-01's detail", m.mode, selected(t, m))
	}
	press(t, m, "esc")
	if msg := ansi.Strip(m.viewMessage()); !strings.Contains(msg, "in Taipei › Prod › DB") {
		t.Errorf("back from the result, the message line is %q", msg)
	}
}

func TestTemplatesBrowseByFolder(t *testing.T) {
	m := filedModel(t)
	press(t, m, "2", "t")
	if msg := ansi.Strip(m.viewMessage()); !strings.Contains(msg, "in Taipei › Templates › Linux") {
		t.Errorf("t on the template opens its folder, message line is %q", msg)
	}
	press(t, m, "esc", "esc")
	if got := strings.Join(rowNames(m.rows()), ","); got != "Templates" {
		t.Errorf("top folder rows = %s, want the Templates folder", got)
	}
}
