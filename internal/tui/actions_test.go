package tui

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// fakeHandoff records what the field-cursor actions asked of the operator's
// workstation instead of touching it — the same reason fakeBackend stands in
// for a real vCenter connection. Its SSH never actually runs anything: see
// the comment on sshCmd in commands.go for why tea.ExecProcess makes that
// safe to call from this synchronous test harness in the first place.
type fakeHandoff struct {
	copied  []string
	opened  []string
	ssh     []SSHSpec
	copyErr error
	openErr error
	sshErr  error
}

func (f *fakeHandoff) Copy(v string) error {
	f.copied = append(f.copied, v)
	return f.copyErr
}

func (f *fakeHandoff) OpenURL(u string) error {
	f.opened = append(f.opened, u)
	return f.openErr
}

func (f *fakeHandoff) SSH(spec SSHSpec) (*exec.Cmd, error) {
	f.ssh = append(f.ssh, spec)
	if f.sshErr != nil {
		return nil, f.sshErr
	}
	return exec.Command("true"), nil
}

// proxiedCtx builds a context routed through an unauthenticated SOCKS5 proxy
// — the case #84 did not account for: the browser cannot follow the route
// vsfleet itself uses, but ssh(1) can be told to.
func proxiedCtx(name, endpoint, proxyAddr string) *config.Context {
	cc := ctx(name, endpoint)
	cc.Transport = config.TransportConfig{Type: config.TransportSOCKS5, Address: proxyAddr}
	return cc
}

// findRow switches to kind and returns the named row, ignoring any jump
// constraint left over from an earlier action in the same test.
func findRow(t *testing.T, m *Model, kind vsphere.Kind, name string) row {
	t.Helper()
	m.kind = kind
	m.jump = nil
	for _, r := range m.rows() {
		if r.name == name {
			return r
		}
	}
	t.Fatalf("row %q of kind %v not found", name, kind)
	return row{}
}

func findAction(items []action, label string) (action, bool) {
	for _, a := range items {
		if a.label == label {
			return a, true
		}
	}
	return action{}, false
}

// TestDetailCursorSkipsDashFields checks that Up/Down never stop on a field
// humanize.Dash blanked out — build-runner-3 has no guest IP, no tools
// state, and no guest state, and none of those should ever be reachable by
// the field cursor, only by paging past them.
func TestDetailCursorSkipsDashFields(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod"})
	press(t, m, "down", "enter") // app-01, then build-runner-3
	r, ok := m.currentRow()
	if !ok || r.name != "build-runner-3" {
		t.Fatalf("expected build-runner-3 detail, got %+v ok=%v", r, ok)
	}
	focusable := detailFocusable(r)
	sawIPAddress := false
	for i := 0; i < len(focusable)+3; i++ {
		if !focusable[m.detailCursor] {
			t.Fatalf("cursor landed on non-focusable line %d after %d presses", m.detailCursor, i)
		}
		if m.detailCursor >= 2 && r.detail[m.detailCursor-2].label == "IP address" {
			sawIPAddress = true
		}
		press(t, m, "down")
	}
	if sawIPAddress {
		t.Fatalf("cursor should never stop on build-runner-3's blank IP address field")
	}
}

// TestManagedObjectFieldRunsSingleActionImmediately checks the "one action
// runs, two or more open a list" rule: the Managed object field offers only
// Copy MoRef, so Enter there must copy without opening a popup.
func TestManagedObjectFieldRunsSingleActionImmediately(t *testing.T) {
	fake := &fakeHandoff{}
	m := newTestModel(t, twoHealthy(), Options{Current: "prod", Handoff: fake})
	r := findRow(t, m, vsphere.KindVM, "app-01")
	idx := -1
	for i, f := range r.detail {
		if f.label == "Managed object" {
			idx = 2 + i
		}
	}
	if idx < 0 {
		t.Fatal("fixture VM has no Managed object field")
	}
	press(t, m, "enter") // open the detail pane
	m.detailCursor = idx
	press(t, m, "enter")
	if m.actions != nil {
		t.Fatalf("a field with exactly one action must not open a popup, got %+v", m.actions)
	}
	if len(fake.copied) != 1 || fake.copied[0] != r.target.moref {
		t.Fatalf("expected the MoRef %q copied once, got %v", r.target.moref, fake.copied)
	}
}

// TestIPFieldOpensPopupWithThreeActions checks the other half of that rule:
// a VM's IP address offers SSH, a copyable ssh command, and Copy value, so
// Enter there must open a popup rather than guessing which one is meant.
func TestIPFieldOpensPopupWithThreeActions(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod"})
	r := findRow(t, m, vsphere.KindVM, "app-01")
	idx := -1
	for i, f := range r.detail {
		if f.label == "IP address" {
			idx = 2 + i
		}
	}
	press(t, m, "enter")
	m.detailCursor = idx
	press(t, m, "enter")
	if m.actions == nil {
		t.Fatal("expected the IP address field to open a popup")
	}
	wantLabels := []string{"SSH to " + r.target.address, "Copy ssh " + r.target.address, "Copy value"}
	for _, want := range wantLabels {
		if _, ok := findAction(m.actions.items, want); !ok {
			var got []string
			for _, a := range m.actions.items {
				got = append(got, a.label)
			}
			t.Errorf("missing action %q, got %v", want, got)
		}
	}
}

// TestJumpFromHostShowsOnlyItsVMs exercises the cross-resource jump: from
// the host's own header, "Show VMs on this host" must narrow the VM table to
// exactly the VMs whose Host field names it — vm.Host does not contain the
// host's own name as a substring, so the plain text filter could never do
// this on its own.
func TestJumpFromHostShowsOnlyItsVMs(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod"})
	press(t, m, "3", "enter") // Hosts tab, the fixture's one host: esxi-01
	r, ok := m.currentRow()
	if !ok || r.kind != vsphere.KindHost || r.name != "esxi-01" {
		t.Fatalf("expected esxi-01 host detail, got %+v ok=%v", r, ok)
	}
	press(t, m, "enter") // object header's action list
	if m.actions == nil {
		t.Fatal("expected the host header to open a popup")
	}
	idx := -1
	for i, a := range m.actions.items {
		if a.label == "Show VMs on this host" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("no jump action offered on the host header: %+v", m.actions.items)
	}
	m.actions.cursor = idx
	press(t, m, "enter")

	if m.jump == nil || m.kind != vsphere.KindVM {
		t.Fatalf("expected a VM jump after running the action, got jump=%+v kind=%v", m.jump, m.kind)
	}
	names := map[string]bool{}
	for _, row := range m.rows() {
		names[row.name] = true
	}
	if !names["app-01"] || names["build-runner-3"] {
		t.Fatalf("jump should show only the VMs on esxi-01 (app-01), got %v", names)
	}

	// Esc clears the jump before it clears anything else.
	press(t, m, "esc")
	if m.jump != nil {
		t.Fatalf("esc should have cleared the jump")
	}
	names = map[string]bool{}
	for _, row := range m.rows() {
		names[row.name] = true
	}
	if !names["build-runner-3"] {
		t.Fatalf("clearing the jump should restore build-runner-3 to the VM table, got %v", names)
	}
}

// TestDemoDisablesExternalActions checks that "vsfleet demo dials nothing"
// still holds for the detail pane: SSH and the browser links must be listed
// and disabled, never silently omitted or, worse, actually launched.
func TestDemoDisablesExternalActions(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod", Demo: true})
	r := findRow(t, m, vsphere.KindVM, "app-01")
	items := m.actionsFor(r, 0)

	ssh, ok := findAction(items, "SSH to "+r.target.address)
	if !ok || ssh.disabled != demoDisabledReason {
		t.Errorf("expected SSH disabled with %q in demo, got %+v", demoDisabledReason, ssh)
	}
	open, ok := findAction(items, "Open in vSphere Client")
	if !ok || open.disabled != demoDisabledReason {
		t.Errorf("expected the browser link disabled with %q in demo, got %+v", demoDisabledReason, open)
	}
	copyMoref, ok := findAction(items, "Copy MoRef")
	if !ok || copyMoref.disabled != "" {
		t.Errorf("copying is not an external command and must stay enabled in demo, got %+v", copyMoref)
	}
}

// TestProxiedContextDisablesBrowserActionsButNotSSH checks the case #84's
// original design did not account for: a proxied vCenter has no route for
// the operator's own browser, but ssh(1) can still be told to follow the
// same proxy, so only the browser links should decline.
func TestProxiedContextDisablesBrowserActionsButNotSSH(t *testing.T) {
	name := "osaka"
	b := &fakeBackend{
		contexts:    []*config.Context{proxiedCtx(name, "https://vcsa.osaka.internal", "127.0.0.1:1080")},
		inventories: map[string]*vsphere.Inventory{name: inventoryFor(name)},
		instanceIDs: map[string]string{name: "52-aaaa-bbbb"},
	}
	m := newTestModel(t, b, Options{Current: name})
	r := findRow(t, m, vsphere.KindVM, "app-01")
	items := m.actionsFor(r, 0)

	open, ok := findAction(items, "Open in vSphere Client")
	if !ok || !strings.Contains(open.disabled, "proxied") {
		t.Errorf("expected the browser link disabled for a proxied context, got %+v", open)
	}
	ssh, ok := findAction(items, "SSH to "+r.target.address)
	if !ok || ssh.disabled != "" {
		t.Errorf("SSH should still be offered through the proxy, got %+v", ssh)
	}
}

// TestActionPopupFitsTerminalWidth guards the popup the way
// TestDetailViewNeverExceedsWidth already guards the rest of the interface:
// a long label plus a long URL must truncate rather than wrap the frame.
func TestActionPopupFitsTerminalWidth(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod"})
	m.width, m.height = 80, 24
	r := findRow(t, m, vsphere.KindVM, "app-01")
	press(t, m, "enter")
	m.detailCursor = 0
	press(t, m, "enter")
	if m.actions == nil {
		t.Fatal("expected the object header to open a popup")
	}
	_ = r
	for _, line := range m.actionListLines() {
		if w := ansi.StringWidth(line); w > m.width {
			t.Errorf("action popup line exceeds terminal width (%d > %d): %q", w, m.width, line)
		}
	}
}
