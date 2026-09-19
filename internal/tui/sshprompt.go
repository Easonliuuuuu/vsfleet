package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// sshPromptState is the "SSH as a different user…" overlay. It exists because
// nothing in vSphere knows a guest's login: VMware Tools reports no account
// names, and the web console only ever shows a prompt the guest itself drew.
// When neither vsfleet's config nor ~/.ssh/config supplies the right user,
// the operator is the only source, so they are asked in place.
//
// It follows credPromptState's idiom — non-nil on Model exactly while it is
// open, and then owning every keystroke — but sits below it: a credential
// request answers a background load and can appear over any screen, so it
// must keep winning.
type sshPromptState struct {
	spec  SSHSpec
	key   string
	input textinput.Model
	// prefill is what the field started with and explicit says whether that
	// was a user vsfleet supplied (a remembered or configured one) rather than
	// one read back from ssh(1). Enter on an untouched, resolved value must
	// not pin it: connecting with no user keeps ~/.ssh/config authoritative,
	// which is the whole reason SSHSpec.User is normally left empty.
	prefill  string
	explicit bool
	err      string
}

func newSSHPromptState(spec SSHSpec, key, prefill string, explicit bool) *sshPromptState {
	ti := newFormInput("user", 40)
	ti.SetValue(prefill)
	ti.CursorEnd()
	ti.Focus()
	return &sshPromptState{spec: spec, key: key, input: ti, prefill: prefill, explicit: explicit}
}

// handleSSHPromptKey drives the overlay. Enter connects, and remembers what
// was typed for this machine; Esc abandons it. Every other key, including
// ones that are global shortcuts elsewhere, goes to the field.
func (m *Model) handleSSHPromptKey(msg tea.KeyMsg) tea.Cmd {
	p := m.sshPrompt
	switch msg.Type {
	case tea.KeyEsc:
		m.sshPrompt = nil
		return nil
	case tea.KeyEnter:
		user := strings.TrimSpace(p.input.Value())
		if user != "" && !validSSHUser(user) {
			p.err = "not a valid user name"
			return nil
		}
		m.sshPrompt = nil
		spec := p.spec
		switch {
		case user == p.prefill && !p.explicit:
			// Untouched resolved value: leave the choice to ssh(1).
			spec.User = ""
		case user == "":
			// Blanked on purpose: forget the remembered user and let
			// ssh(1) decide again.
			spec.User = ""
			m.forgetSSHUser(p.key)
		default:
			spec.User = user
			m.rememberSSHUser(p.key, user)
		}
		return m.sshCmd(spec)
	}
	p.err = ""
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return cmd
}

func (m *Model) rememberSSHUser(key, user string) {
	if key == "" {
		return
	}
	if m.sshUsers == nil {
		m.sshUsers = map[string]string{}
	}
	m.sshUsers[key] = user
}

func (m *Model) forgetSSHUser(key string) {
	delete(m.sshUsers, key)
}

// viewSSHPrompt renders the overlay.
func (m *Model) viewSSHPrompt() []string {
	t := m.theme
	p := m.sshPrompt
	p.input.Width = clamp(m.width-2, 10, 40)
	lines := []string{
		t.title.Render("SSH to " + p.spec.Address),
		"",
		"  " + p.input.View(),
		"",
	}
	if p.err != "" {
		lines = append(lines, "  "+t.bad.Render(p.err), "")
	}
	for _, l := range wrap("vSphere does not know this machine's login, so it is asked here. What you type is remembered for this machine. Leave it blank to let ssh choose from ~/.ssh/config.", m.width-2) {
		lines = append(lines, "  "+t.dim.Render(l))
	}
	lines = append(lines, "")
	lines = append(lines, m.overlayKeys([][2]string{{"enter", "connect"}, {"esc", "cancel"}, {"ctrl+c", "quit"}})...)
	return scrollLines(lines, 0, m.bodyHeight())
}
