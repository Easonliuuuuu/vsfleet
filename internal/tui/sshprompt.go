package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type sshPromptFocus uint8

const (
	sshFocusUser sshPromptFocus = iota
	sshFocusIdentity
	sshFocusPath
)

// sshPromptState is the combined SSH connection overlay. It owns both the
// remote login and the private-key choice before the terminal is handed to
// ssh(1), so confirming a username cannot unexpectedly launch a process.
type sshPromptState struct {
	spec         SSHSpec
	userKey      string
	identityKey  string
	input        textinput.Model
	pathInput    textinput.Model
	prefill      string
	explicit     bool
	focus        sshPromptFocus
	identities   []SSHIdentity
	remembered   string
	cursor       int
	loading      bool
	discoveryErr error
	err          string
}

func newSSHPromptState(spec SSHSpec, userKey, prefill string, explicit bool, identityKey string) *sshPromptState {
	ti := newFormInput("user", 40)
	ti.SetValue(prefill)
	ti.CursorEnd()
	ti.Focus()
	path := newFormInput("~/.ssh/id_ed25519", 60)
	path.Blur()
	return &sshPromptState{
		spec: spec, userKey: userKey, identityKey: identityKey,
		input: ti, pathInput: path, prefill: prefill, explicit: explicit,
		focus: sshFocusUser, remembered: spec.IdentityFile, loading: true,
	}
}

// identityChoices always includes the OpenSSH default and a manual path row.
// A remembered path that disappeared is retained as a visible, fixable row.
func (p *sshPromptState) identityChoices() []SSHIdentity {
	choices := []SSHIdentity{{Path: ""}}
	seen := map[string]bool{"": true}
	for _, item := range p.identities {
		path, err := expandSSHPath(item.Path)
		if err != nil || seen[path] {
			continue
		}
		seen[path] = true
		choices = append(choices, SSHIdentity{Path: path})
	}
	if p.remembered != "" {
		path, err := expandSSHPath(p.remembered)
		if err == nil && !seen[path] {
			choices = append(choices, SSHIdentity{Path: path})
		}
	}
	// The final empty-path row is the manual-entry action, distinguished by
	// its position rather than by a sentinel path.
	choices = append(choices, SSHIdentity{Path: ""})
	return choices
}

func (p *sshPromptState) identityIsManual() bool {
	return p.cursor == len(p.identityChoices())-1
}

func (p *sshPromptState) selectedIdentity() string {
	choices := p.identityChoices()
	if p.cursor < 0 || p.cursor >= len(choices) || p.identityIsManual() {
		return ""
	}
	return choices[p.cursor].Path
}

// handleSSHPromptKey drives the combined overlay. Enter on the username moves
// to the identity list; only an identity choice or a confirmed manual path
// starts ssh(1). Every other key remains inside the overlay.
func (m *Model) handleSSHPromptKey(msg tea.KeyMsg) tea.Cmd {
	p := m.sshPrompt
	if p == nil {
		return nil
	}
	if msg.Type == tea.KeyEsc {
		if p.focus == sshFocusPath {
			p.focus = sshFocusIdentity
			p.pathInput.Blur()
			return nil
		}
		m.sshPrompt = nil
		return nil
	}
	if msg.Type == tea.KeyTab || msg.Type == tea.KeyShiftTab {
		if p.focus == sshFocusPath {
			p.focus = sshFocusIdentity
			p.pathInput.Blur()
			return nil
		}
		if msg.Type == tea.KeyShiftTab {
			p.focus = sshFocusUser
		} else if p.focus == sshFocusUser {
			p.focus = sshFocusIdentity
		} else {
			p.focus = sshFocusUser
		}
		p.syncFocus()
		return nil
	}
	if p.focus == sshFocusIdentity {
		switch msg.Type {
		case tea.KeyUp:
			p.cursor = clamp(p.cursor-1, 0, len(p.identityChoices())-1)
			return nil
		case tea.KeyDown:
			p.cursor = clamp(p.cursor+1, 0, len(p.identityChoices())-1)
			return nil
		case tea.KeyEnter:
			if p.identityIsManual() {
				p.focus = sshFocusPath
				p.pathInput.Focus()
				return nil
			}
			return m.connectFromSSHPrompt(p, p.selectedIdentity())
		}
	}
	if p.focus == sshFocusPath {
		if msg.Type == tea.KeyEnter {
			path := strings.TrimSpace(p.pathInput.Value())
			if path == "" {
				p.err = "type a private-key path"
				return nil
			}
			expanded, err := expandSSHPath(path)
			if err != nil {
				p.err = err.Error()
				return nil
			}
			if _, ok := existingSSHIdentity(expanded); !ok {
				p.err = fmt.Sprintf("SSH identity %q is not a regular file", displaySSHPath(expanded))
				return nil
			}
			return m.connectFromSSHPrompt(p, expanded)
		}
		p.err = ""
		var cmd tea.Cmd
		p.pathInput, cmd = p.pathInput.Update(msg)
		return cmd
	}

	if msg.Type == tea.KeyEnter {
		p.focus = sshFocusIdentity
		p.syncFocus()
		return nil
	}
	p.err = ""
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return cmd
}

func (p *sshPromptState) syncFocus() {
	if p.focus == sshFocusUser {
		p.input.Focus()
	} else {
		p.input.Blur()
	}
	if p.focus != sshFocusPath {
		p.pathInput.Blur()
	}
}

func (m *Model) connectFromSSHPrompt(p *sshPromptState, identity string) tea.Cmd {
	user := strings.TrimSpace(p.input.Value())
	if user != "" && !validSSHUser(user) {
		p.err = "not a valid user name"
		return nil
	}
	m.sshPrompt = nil
	spec := p.spec
	switch {
	case user == p.prefill && !p.explicit:
		spec.User = ""
	case user == "":
		spec.User = ""
		m.forgetSSHUser(p.userKey)
	default:
		spec.User = user
		m.rememberSSHUser(p.userKey, user)
	}
	if identity == "" {
		spec.IdentityFile = ""
		m.forgetSSHIdentity(p.identityKey)
	} else {
		spec.IdentityFile = identity
		m.rememberSSHIdentity(p.identityKey, identity)
	}
	return m.sshCmd(spec)
}

func (m *Model) openSSHPrompt(spec SSHSpec, userKey, prefill string, explicit bool, identityKey string) tea.Cmd {
	p := newSSHPromptState(spec, userKey, prefill, explicit, identityKey)
	m.sshPrompt = p
	return tea.Batch(discoverSSHIdentitiesCmd(m.handoff, p), m.spin.Tick)
}

func discoverSSHIdentitiesCmd(h Handoff, p *sshPromptState) tea.Cmd {
	return func() tea.Msg {
		identities, err := h.DiscoverSSHIdentities(p.spec.Address)
		return sshIdentitiesMsg{prompt: p, identities: identities, err: err}
	}
}

func (m *Model) applySSHIdentities(msg sshIdentitiesMsg) tea.Cmd {
	if m.sshPrompt == nil || m.sshPrompt != msg.prompt {
		return nil
	}
	p := m.sshPrompt
	p.loading = false
	p.discoveryErr = msg.err
	p.identities = msg.identities
	choices := p.identityChoices()
	p.cursor = 0
	for i, item := range choices {
		if item.Path != "" && item.Path == p.remembered {
			p.cursor = i
			break
		}
	}
	return nil
}

func (m *Model) rememberSSHIdentity(key, path string) {
	if key == "" {
		return
	}
	if m.sshIdentityFiles == nil {
		m.sshIdentityFiles = map[string]string{}
	}
	m.sshIdentityFiles[key] = path
}

func (m *Model) forgetSSHIdentity(key string) {
	delete(m.sshIdentityFiles, key)
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

// viewSSHPrompt renders the combined user and identity chooser.
func (m *Model) viewSSHPrompt() []string {
	t := m.theme
	p := m.sshPrompt
	p.input.Width = clamp(m.width-2, 10, 40)
	p.pathInput.Width = clamp(m.width-2, 20, 70)
	lines := []string{t.title.Render("SSH to " + p.spec.Address), ""}
	userLabel := "  " + t.label.Render("User") + "  "
	if p.focus == sshFocusUser {
		userLabel = "▸ " + t.focused.Render("User") + "  "
	}
	lines = append(lines, userLabel+p.input.View(), "", "  "+t.label.Render("Identity"))
	choices := p.identityChoices()
	for i, item := range choices {
		marker := "  "
		if p.focus == sshFocusIdentity && i == p.cursor {
			marker = "▸ "
		}
		label := "OpenSSH default"
		switch {
		case i == len(choices)-1:
			label = "Enter another path…"
		case item.Path != "":
			label = displaySSHPath(item.Path)
			if item.Path == p.remembered {
				if _, ok := existingSSHIdentity(item.Path); !ok {
					label += " (missing)"
				}
			}
		}
		lines = append(lines, marker+label)
	}
	if p.focus == sshFocusPath {
		lines = append(lines, "", "  "+t.label.Render("Path")+"  "+p.pathInput.View())
	}
	if p.loading {
		lines = append(lines, "", "  "+m.spin.View()+t.dim.Render(" discovering SSH identities…"))
	}
	if p.discoveryErr != nil && !p.loading {
		lines = append(lines, "", "  "+t.dim.Render("Could not inspect ~/.ssh; manual paths are still available."))
	}
	if p.err != "" {
		lines = append(lines, "", "  "+t.bad.Render(p.err))
	}
	for _, l := range wrap("Tab switches between the username and identity list. Selecting OpenSSH default leaves ~/.ssh/config and ssh-agent in control. An explicit key uses public-key authentication only.", m.width-2) {
		lines = append(lines, "  "+t.dim.Render(l))
	}
	lines = append(lines, "")
	lines = append(lines, m.overlayKeys([][2]string{{"enter", "next / connect"}, {"tab", "switch field"}, {"esc", "cancel"}, {"ctrl+c", "quit"}})...)
	return scrollLines(lines, 0, m.bodyHeight())
}
