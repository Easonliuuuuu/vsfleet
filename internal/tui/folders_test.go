package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// filedModel opens prod's VMs tab on an inventory filed into folders the way
// the vSphere client shows them: jump-01 at the top of Taipei; ops-01 in
// /Prod beside its subfolders, web-01 and web-02 in /Prod/Customer-Facing-Web
// and db-01 in /Prod/DB; api-01 in the vApp web-stack under /Apps, with
// cache-01 in its nested vApp web-cache, and the vApp idle-vapp, with no
// VMs, beside web-stack. Its one template is in /Templates/Linux. dcs adds one
// VM filed at the top of each further datacenter.
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
		vm("Taipei", "ops-01", "/Prod", "esxi-02"),
		vm("Taipei", "web-01", "/Prod/Customer-Facing-Web", "esxi-01"),
		vm("Taipei", "web-02", "/Prod/Customer-Facing-Web", "esxi-02"),
	}
	inv.VMs[1].PowerState = "poweredOff"
	inv.Templates[0].Folder = "/Templates/Linux"
	vapp := func(id, name, folder string) vsphere.VApp {
		return vsphere.VApp{
			Location: vsphere.Location{Context: "prod", Datacenter: "Taipei", Path: "/Taipei/vm" + folder},
			ID:       id, Name: name, Status: "started",
		}
	}
	inv.VApps = append(inv.VApps,
		vapp("resgroup-v1", "web-stack", "/Apps/web-stack"),
		vapp("resgroup-v2", "web-cache", "/Apps/web-stack/web-cache"),
		vapp("resgroup-v3", "idle-vapp", "/Apps/idle-vapp"),
	)
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

// screenLine is the line of the rendered screen that contains s.
func screenLine(t *testing.T, m *Model, s string) string {
	t.Helper()
	out := ansi.Strip(m.View())
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, s) {
			return line
		}
	}
	t.Fatalf("no line contains %q:\n%s", s, out)
	return ""
}

func TestFolderViewBrowsesOneLevelAtATime(t *testing.T) {
	m := filedModel(t, "Tainan")
	requireFooter(t, m, []string{"t folders"}, []string{"1-7 kind"})

	steps := []struct {
		keys     []string
		rows     string
		selected string
		where    string
	}{
		// t opens at the top: two datacenters in scope, so it lists them.
		{[]string{"t"}, "Tainan,Taipei", "Tainan", "in all datacenters"},
		{[]string{"j", "enter"}, "Apps,Prod,jump-01", "Apps", "in Taipei  ·"},
		{[]string{"enter"}, "idle-vapp,web-stack", "idle-vapp", "in Taipei › Apps"},
		// esc climbs back and leaves the cursor on the folder it came from.
		{[]string{"esc"}, "Apps,Prod,jump-01", "Apps", "in Taipei  ·"},
		{[]string{"esc"}, "Tainan,Taipei", "Taipei", "in all datacenters"},
		{[]string{"esc"}, "Tainan,Taipei", "Taipei", "in all datacenters"},
		{[]string{"enter", "j", "enter"}, "Customer-Facing-Web,DB,ops-01", "Customer-Facing-Web", "in Taipei › Prod"},
		{[]string{"enter"}, "web-01,web-02", "web-01", "in Taipei › Prod › Customer-Facing-Web"},
		// t lists every VM again and keeps the selection.
		{[]string{"t"}, "api-01,cache-01,db-01,jump-01,ops-01,web-01,web-02,dr-tainan", "web-01", ""},
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
	requireFooter(t, m, []string{"t folders"}, nil)
}

func TestFolderRowsFitTheirLevel(t *testing.T) {
	m := filedModel(t)
	m.width = 80
	press(t, m, "t")
	requireFooter(t, m, []string{"t list"}, nil)

	// A level of folders and vApps alone takes the folders' own columns:
	// web-stack holds api-01 (on) and cache-01 (off), 2 vCPU and 4G each.
	press(t, m, "enter")
	if h := screenLine(t, m, "NAME"); !strings.Contains(h, "VMS") || !strings.Contains(h, "POWERED ON") || strings.Contains(h, "IP ADDRESS") {
		t.Errorf("a level of folders keeps the VM headings: %q", h)
	}
	line := screenLine(t, m, "web-stack  vApp")
	if got := strings.Fields(line); strings.Join(got[len(got)-4:], " ") != "2 1 4 8G" {
		t.Errorf("web-stack row = %q, want 2 VMs, 1 on, 4 vCPU, 8G", got)
	}
	if h := screenLine(t, m, "NAME"); ansi.StringWidth(strings.TrimRight(line, " ")) != ansi.StringWidth(strings.TrimRight(h, " ")) {
		t.Errorf("the totals are not right-aligned under their headings:\n%s\n%s", h, line)
	}

	// Beside a VM the VM columns stay, and at 80 columns, where NAME is
	// narrower than the label, a folder runs on over the columns it leaves
	// empty instead of cutting its count short.
	press(t, m, "esc", "j", "enter")
	if h := screenLine(t, m, "NAME"); !strings.Contains(h, "IP ADDRESS") {
		t.Errorf("a level with ops-01 loses the VM headings: %q", h)
	}
	screenLine(t, m, "▹ Customer-Facing-Web/  2 VMs")

	press(t, m, "2")
	requireFooter(t, m, []string{"t folders"}, nil)
	press(t, m, "t")
	if h := screenLine(t, m, "NAME"); !strings.Contains(h, "TEMPLATES") {
		t.Errorf("a level of template folders lacks the TEMPLATES heading: %q", h)
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
			want: "db-01,ops-01,web-01,web-02",
			hint: "4 VMs in Taipei › Prod",
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
			// One datacenter: t opens inside it, with no level that lists a
			// single datacenter.
			press(t, m, "t")
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
	press(t, m, "t", "G", "enter")
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
	if got := strings.Join(rowNames(m.rows()), ","); got != "Templates" {
		t.Fatalf("top folder rows = %s, want the Templates folder", got)
	}
	press(t, m, "enter", "enter")
	if got := strings.Join(rowNames(m.rows()), ","); got != "ubuntu-24.04-golden" {
		t.Errorf("rows in /Templates/Linux = %s", got)
	}
	if msg := ansi.Strip(m.viewMessage()); !strings.Contains(msg, "in Taipei › Templates › Linux") {
		t.Errorf("message line is %q", msg)
	}
}

// TestVAppsInTheFolderViewAreVApps pins the vSphere client's treatment: a
// vApp sits in its folder with a vApp's own mark, enter opens the vApp's
// page rather than a level, a vApp with no VMs is still listed, and the
// message line names a vApp level as one.
func TestVAppsInTheFolderViewAreVApps(t *testing.T) {
	m := filedModel(t)
	press(t, m, "t", "enter")
	screenLine(t, m, "▣ web-stack  vApp")
	if line := screenLine(t, m, "idle-vapp"); !strings.Contains(line, "▣ idle-vapp  vApp") {
		t.Errorf("a vApp with no VMs is not marked as one: %q", line)
	}

	press(t, m, "j", "enter")
	if m.mode != modeVAppDetail {
		t.Fatalf("enter on web-stack: mode %v, want its vApp page", m.mode)
	}
	if v, _, ok := m.activeVApp(); !ok || v.Name != "web-stack" {
		t.Fatalf("the vApp page is on %+v, want web-stack", v)
	}
	press(t, m, "esc")
	if m.mode != modeBrowse || selected(t, m) != "web-stack" {
		t.Errorf("esc from the vApp page: mode %v on %q, want the folder view on web-stack", m.mode, selected(t, m))
	}

	// A member reached from search lands inside its vApps.
	press(t, m, "tab")
	typeText(t, m, "cache-01")
	press(t, m, "enter", "enter", "esc")
	if msg := ansi.Strip(m.viewMessage()); !strings.Contains(msg, "in Taipei › Apps › web-stack (vApp) › web-cache (vApp)") {
		t.Errorf("message line inside a nested vApp is %q", msg)
	}
}
