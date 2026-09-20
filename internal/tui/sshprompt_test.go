package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func labels(items []action) []string {
	out := make([]string, len(items))
	for i, a := range items {
		out[i] = a.label
	}
	return out
}

// openSSHPrompt runs "SSH with a different user or key…" for r's first target and
// silences the field's blink timer, which a synchronous harness would block on.
func openSSHPrompt(t *testing.T, m *Model, r row) {
	t.Helper()
	a, ok := findAction(m.actionsFor(r, 0), "SSH with a different user or key…")
	if !ok || a.disabled != "" {
		t.Fatalf("no usable prompt action: %v", labels(m.actionsFor(r, 0)))
	}
	drive(t, m, a.run(m))
	if m.sshPrompt == nil {
		t.Fatal("the action did not open the prompt")
	}
	m.sshPrompt.input.Cursor.SetMode(cursor.CursorStatic)
	m.sshPrompt.pathInput.Cursor.SetMode(cursor.CursorStatic)
}

// vSphere cannot say who a guest's login is, so the label has to say who ssh
// itself will use — and must still leave the choice to ssh.
func TestSSHLabelShowsTheUserSSHWouldResolve(t *testing.T) {
	fake := &fakeHandoff{resolved: map[string]string{"10.20.0.11": "tdclab"}}
	m := newTestModel(t, twoHealthy(), Options{Current: "prod", Handoff: fake})
	r := findRow(t, m, vsphere.KindVM, "app-01")

	a := m.sshAction(r, r.target.address)
	if a.label != "SSH to tdclab@10.20.0.11" || a.detail != "ssh -o ConnectTimeout=15 tdclab@10.20.0.11" {
		t.Fatalf("label/detail = %q / %q", a.label, a.detail)
	}
	a.run(m)
	if len(fake.ssh) != 1 || fake.ssh[0].User != "" || fake.ssh[0].Address != "10.20.0.11" {
		t.Fatalf("a resolved user must not be pinned into the spec: %+v", fake.ssh)
	}
}

func TestSSHLabelWithoutAResolvedUserIsTheBareAddress(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod"})
	r := findRow(t, m, vsphere.KindVM, "app-01")
	if got := m.sshAction(r, r.target.address).label; got != "SSH to 10.20.0.11" {
		t.Errorf("label = %q", got)
	}
}

func TestDemoNeverAsksSSHForItsConfiguration(t *testing.T) {
	fake := &fakeHandoff{}
	m := newTestModel(t, twoHealthy(), Options{Current: "prod", Handoff: fake, Demo: true})
	r := findRow(t, m, vsphere.KindVM, "app-01")
	for _, a := range m.actionsFor(r, 0) {
		if strings.HasPrefix(a.label, "SSH") && a.disabled == "" {
			t.Errorf("%q must be disabled in demo mode", a.label)
		}
	}
	m.sshCommandCopyAction(r, r.target.address)
	if len(fake.resolveCalls) != 0 {
		t.Errorf("demo ran ssh -G for %v", fake.resolveCalls)
	}
}

func TestVMOffersItsNameAndItsIP(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod"})
	r := findRow(t, m, vsphere.KindVM, "app-01")

	r.target.hostName = "app-01.lab.example"
	want := []string{"SSH to app-01.lab.example", "SSH to 10.20.0.11", "SSH with a different user or key…"}
	if got := labels(m.vmSSHActions(r)); !reflect.DeepEqual(got, want) {
		t.Errorf("actions = %v, want %v", got, want)
	}

	// The prompt aims at the name, the target ~/.ssh/config can match.
	openSSHPrompt(t, m, r)
	if m.sshPrompt.spec.Address != "app-01.lab.example" {
		t.Errorf("prompt targets %q", m.sshPrompt.spec.Address)
	}

	for _, name := range []string{"localhost", "localhost.localdomain"} {
		r.target.hostName = name
		if got := labels(m.vmSSHActions(r)); got[0] != "SSH to 10.20.0.11" {
			t.Errorf("%q must not be offered, got %v", name, got)
		}
	}
	r.target.hostName = "10.20.0.11"
	if got := labels(m.vmSSHActions(r)); len(got) != 2 {
		t.Errorf("a name equal to the IP must not be listed twice: %v", got)
	}
	r.target.hostName, r.target.address = "app-01.lab.example", ""
	if got := labels(m.vmSSHActions(r)); got[0] != "SSH to app-01.lab.example" || len(got) != 2 {
		t.Errorf("a VM with a name but no IP should still offer SSH: %v", got)
	}
}

func TestSSHUserPrecedence(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod", SSHUser: "legacy", SSHVMUser: "ubuntu"})
	r := findRow(t, m, vsphere.KindVM, "app-01")
	other := findRow(t, m, vsphere.KindVM, "build-runner-3")

	if got := m.sshUserFor(r); got != "ubuntu" {
		t.Errorf("configured per-kind user = %q", got)
	}
	m.rememberSSHUser(sshUserKey(r), "tdclab")
	if got := m.sshUserFor(r); got != "tdclab" {
		t.Errorf("remembered user = %q, want it to beat the config", got)
	}
	if got := m.sshUserFor(other); got != "ubuntu" {
		t.Errorf("a remembered user leaked onto another machine: %q", got)
	}
	m.forgetSSHUser(sshUserKey(r))
	if got := m.sshUserFor(r); got != "ubuntu" {
		t.Errorf("after forgetting = %q", got)
	}
}

func TestSSHPromptConnectsAsTheTypedUserAndRemembersIt(t *testing.T) {
	fake := &fakeHandoff{}
	m := newTestModel(t, twoHealthy(), Options{Current: "prod", Handoff: fake})
	r := findRow(t, m, vsphere.KindVM, "app-01")
	openSSHPrompt(t, m, r)

	typeText(t, m, "tdclab")
	if !strings.Contains(m.View(), "SSH to 10.20.0.11") {
		t.Errorf("the overlay should name its target:\n%s", m.View())
	}
	if cmd := m.handleSSHPromptKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("the first Enter should move to identity selection")
	}
	if cmd := m.handleSSHPromptKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Fatal("the second Enter should start the ssh session")
	}
	if m.sshPrompt != nil {
		t.Error("the prompt should close on Enter")
	}
	if len(fake.ssh) != 1 || fake.ssh[0].User != "tdclab" || fake.ssh[0].Address != "10.20.0.11" {
		t.Fatalf("ssh specs = %+v", fake.ssh)
	}
	if got := m.Snapshot().SSHUsers[sshUserKey(r)]; got != "tdclab" {
		t.Errorf("snapshot did not carry the remembered user: %v", m.Snapshot().SSHUsers)
	}
	// The next connection to this machine uses it without asking.
	if a := m.sshAction(r, r.target.address); a.label != "SSH to tdclab@10.20.0.11" {
		t.Errorf("label after remembering = %q", a.label)
	}
}

func TestSSHPromptEscapeConnectsToNothing(t *testing.T) {
	fake := &fakeHandoff{}
	m := newTestModel(t, twoHealthy(), Options{Current: "prod", Handoff: fake})
	openSSHPrompt(t, m, findRow(t, m, vsphere.KindVM, "app-01"))
	typeText(t, m, "nobody")
	press(t, m, "esc")
	if m.sshPrompt != nil || len(fake.ssh) != 0 || len(m.Snapshot().SSHUsers) != 0 {
		t.Errorf("Esc must cancel cleanly: prompt=%v ssh=%v users=%v", m.sshPrompt, fake.ssh, m.Snapshot().SSHUsers)
	}
}

// Accepting the value ssh(1) reported must not pin it: it would silently stop
// following ~/.ssh/config the day that changes.
func TestSSHPromptLeavesAnUntouchedResolvedUserToSSH(t *testing.T) {
	fake := &fakeHandoff{resolved: map[string]string{"10.20.0.11": "eason"}}
	m := newTestModel(t, twoHealthy(), Options{Current: "prod", Handoff: fake})
	openSSHPrompt(t, m, findRow(t, m, vsphere.KindVM, "app-01"))
	if got := m.sshPrompt.input.Value(); got != "eason" {
		t.Fatalf("prefill = %q, want the resolved user", got)
	}
	press(t, m, "enter", "enter")
	if len(fake.ssh) != 1 || fake.ssh[0].User != "" || len(m.Snapshot().SSHUsers) != 0 {
		t.Errorf("resolved user was pinned: ssh=%+v users=%v", fake.ssh, m.Snapshot().SSHUsers)
	}
}

func TestSSHPromptBlankForgetsARememberedUser(t *testing.T) {
	fake := &fakeHandoff{}
	m := newTestModel(t, twoHealthy(), Options{Current: "prod", Handoff: fake})
	r := findRow(t, m, vsphere.KindVM, "app-01")
	m.rememberSSHUser(sshUserKey(r), "tdclab")
	openSSHPrompt(t, m, r)
	if got := m.sshPrompt.input.Value(); got != "tdclab" {
		t.Fatalf("prefill = %q, want the remembered user", got)
	}
	for range "tdclab" {
		drive(t, m, discard(m.Update(tea.KeyMsg{Type: tea.KeyBackspace})))
	}
	press(t, m, "enter", "enter")
	if len(fake.ssh) != 1 || fake.ssh[0].User != "" || len(m.Snapshot().SSHUsers) != 0 {
		t.Errorf("ssh=%+v users=%v", fake.ssh, m.Snapshot().SSHUsers)
	}
}

func TestSSHPromptRefusesAUserSSHWouldReadAsAnOption(t *testing.T) {
	fake := &fakeHandoff{}
	m := newTestModel(t, twoHealthy(), Options{Current: "prod", Handoff: fake})
	openSSHPrompt(t, m, findRow(t, m, vsphere.KindVM, "app-01"))
	typeText(t, m, "-oProxyCommand=x")
	press(t, m, "enter", "enter")
	if m.sshPrompt == nil || m.sshPrompt.err == "" || len(fake.ssh) != 0 {
		t.Fatalf("an option-shaped user must be refused in place: prompt=%+v ssh=%v", m.sshPrompt, fake.ssh)
	}
}

// A user name can contain any letter, including the ones that are global
// shortcuts everywhere else.
func TestSSHPromptOwnsKeystrokes(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod"})
	openSSHPrompt(t, m, findRow(t, m, vsphere.KindVM, "app-01"))
	press(t, m, "q")
	if m.quitting || m.sshPrompt == nil {
		t.Fatal(`"q" quit or closed the overlay instead of being typed`)
	}
	if got := m.sshPrompt.input.Value(); got != "q" {
		t.Errorf("field = %q", got)
	}
}

// A credential request answers a background load and can appear over any
// screen, so it must keep winning the keyboard.
func TestCredentialPromptOutranksTheSSHPrompt(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{Current: "prod"})
	openSSHPrompt(t, m, findRow(t, m, vsphere.KindVM, "app-01"))
	m.credPrompt = newCredPromptState(credRequest{label: "lab", resp: make(chan credResult, 1)})
	m.credPrompt.input.Cursor.SetMode(cursor.CursorStatic)

	if !strings.Contains(m.View(), "Password for lab") {
		t.Errorf("the credential overlay should be on top:\n%s", m.View())
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if m.credPrompt.input.Value() != "x" || m.sshPrompt.input.Value() != "" {
		t.Errorf("key went to the wrong overlay: cred=%q ssh=%q", m.credPrompt.input.Value(), m.sshPrompt.input.Value())
	}
}

func TestRememberedUsersSeedAndRoundTripThroughTheSnapshot(t *testing.T) {
	m := newTestModel(t, twoHealthy(), Options{
		Current:  "prod",
		SSHUsers: map[string]string{"prod/prod-vm-1": "tdclab", "prod/gone": ""},
	})
	if got := m.Snapshot().SSHUsers; !reflect.DeepEqual(got, map[string]string{"prod/prod-vm-1": "tdclab"}) {
		t.Errorf("snapshot users = %v", got)
	}
	// The snapshot is a copy: mutating it must not reach the model.
	m.Snapshot().SSHUsers["prod/prod-vm-1"] = "mallory"
	if got := m.sshUsers["prod/prod-vm-1"]; got != "tdclab" {
		t.Errorf("snapshot aliases the model's map: %q", got)
	}
}

// Both strings reach ssh(1)'s argument list and one of them is chosen by the
// guest, so the real handoff must refuse anything readable as an option.
func TestRealHandoffRefusesOptionShapedTargets(t *testing.T) {
	h := realHandoff{}
	for _, spec := range []SSHSpec{
		{Address: "-oProxyCommand=touch /tmp/pwned"},
		{Address: "host name"},
		{Address: "host", User: "-oProxyCommand=x"},
		{Address: "host", User: "a b"},
		{Address: "ho\x1bst"},
	} {
		if cmd, err := h.SSH(spec); err == nil {
			t.Errorf("SSH(%+v) built %v, want a refusal", spec, cmd.Args)
		}
	}
	if h.ResolveUser("-oProxyCommand=x") != "" {
		t.Error("ResolveUser passed an option-shaped destination to ssh -G")
	}
	cmd, err := h.SSH(SSHSpec{Address: "host.example", User: "corp@example.com"})
	if err != nil || cmd.Args[len(cmd.Args)-1] != "corp@example.com@host.example" {
		t.Errorf("a UPN-style user should still work: %v %v", cmd, err)
	}
}

func TestParseSSHUser(t *testing.T) {
	out := "host 10.0.0.5\r\nuser tdclab\r\nhostname 10.0.0.5\nport 22\n"
	if got := parseSSHUser(out); got != "tdclab" {
		t.Errorf("got %q", got)
	}
	if got := parseSSHUser("port 22\n"); got != "" {
		t.Errorf("got %q for output with no user line", got)
	}
}

func TestParseSSHIdentityFiles(t *testing.T) {
	out := "identityfile ~/.ssh/id_rsa\nidentityfile /tmp/id_devops\nidentityfile %d/.ssh/id_ed25519\n"
	got := parseSSHIdentityFiles(out)
	want := []string{"~/.ssh/id_rsa", "/tmp/id_devops"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("identity files = %v, want %v", got, want)
	}
}

func TestExplicitSSHIdentityBuildsPublicKeyOnlyCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id_devops")
	if err := os.WriteFile(path, []byte("synthetic key path\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd, err := (realHandoff{}).SSH(SSHSpec{Address: "host.example", User: "ubuntu", IdentityFile: path})
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.Args, " ")
	for _, want := range []string{
		"-o ConnectTimeout=15", "-i " + path, "-o IdentitiesOnly=yes",
		"-o PreferredAuthentications=publickey", "-o PasswordAuthentication=no",
		"-o KbdInteractiveAuthentication=no", "ubuntu@host.example",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("SSH args %q do not contain %q", args, want)
		}
	}
}

func TestSSHPromptRemembersSelectedIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id_devops")
	if err := os.WriteFile(path, []byte("synthetic key path\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeHandoff{identities: []SSHIdentity{{Path: path}}}
	m := newTestModel(t, twoHealthy(), Options{Current: "prod", Handoff: fake})
	r := findRow(t, m, vsphere.KindVM, "app-01")
	openSSHPrompt(t, m, r)
	// Discovery selects OpenSSH default initially; move to the discovered key.
	press(t, m, "enter", "down", "enter")
	if len(fake.ssh) != 1 || fake.ssh[0].IdentityFile != path {
		t.Fatalf("ssh specs = %+v", fake.ssh)
	}
	if got := m.Snapshot().SSHIdentityFiles[sshIdentityKey(r)]; got != path {
		t.Fatalf("remembered identity = %q, want %q", got, path)
	}
}

func TestSSHPromptEnterOnUsernameDoesNotConnect(t *testing.T) {
	fake := &fakeHandoff{}
	m := newTestModel(t, twoHealthy(), Options{Current: "prod", Handoff: fake})
	openSSHPrompt(t, m, findRow(t, m, vsphere.KindVM, "app-01"))
	if cmd := m.handleSSHPromptKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || len(fake.ssh) != 0 || m.sshPrompt.focus != sshFocusIdentity {
		t.Fatalf("username Enter launched SSH: cmd=%v ssh=%v focus=%v", cmd, fake.ssh, m.sshPrompt.focus)
	}
}
