package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/uistate"
)

type sshPromptFocus uint8

const (
	sshFocusDestination sshPromptFocus = iota
	sshFocusRoute
	sshFocusProxyAddr
	sshFocusUser
	sshFocusIdentity
	sshFocusPath
)

// destKind distinguishes an OpenSSH alias — a complete destination and
// route on its own — from one of the VM's own native addresses, which still
// needs a route chosen (or left automatic).
type destKind uint8

const (
	destAlias destKind = iota
	destNative
)

// destinationChoice is one row of the prompt's Destination list.
type destinationChoice struct {
	kind destKind
	// value is the OpenSSH alias name (destAlias) or the native DNS name or
	// IP address (destNative) this choice connects to.
	value string
	label string
}

// routeOrder is every per-VM route override the Route section offers, in
// display order. "" is "Automatic": the same #178/context precedence
// sshSpec's own fallback already applies, not a fourth kind of its own.
var routeOrder = []string{"", sshRouteOpenSSH, sshRouteDirect, config.TransportHTTPProxy, config.TransportSOCKS5}

func routeLabel(kind string) string {
	switch kind {
	case "":
		return "Automatic (#178 routes, then the context's own transport)"
	case sshRouteOpenSSH:
		return "OpenSSH default (no vsfleet route — ~/.ssh/config decides)"
	case sshRouteDirect:
		return "Direct (bypass ~/.ssh/config's own ProxyJump/ProxyCommand)"
	case config.TransportHTTPProxy:
		return "HTTP CONNECT…"
	case config.TransportSOCKS5:
		return "SOCKS5…"
	default:
		return kind
	}
}

func routeNeedsProxyAddr(kind string) bool {
	return kind == config.TransportHTTPProxy || kind == config.TransportSOCKS5
}

// sshPromptState is the combined SSH connection overlay. It owns the
// destination, route, remote login and private-key choice before the
// terminal is handed to ssh(1), so confirming one field cannot unexpectedly
// launch a process on a combination the operator never saw together.
type sshPromptState struct {
	row  row
	spec SSHSpec

	userKey     string
	identityKey string

	input      textinput.Model
	pathInput  textinput.Model
	proxyInput textinput.Model
	prefill    string
	explicit   bool
	focus      sshPromptFocus

	identities   []SSHIdentity
	remembered   string
	cursor       int
	loading      bool
	discoveryErr error

	destinations []destinationChoice
	destCursor   int
	destErr      error

	route string

	err string
}

func newSSHPromptState(r row, spec SSHSpec, userKey, prefill string, explicit bool, identityKey string, info sshDestInfo, remembered uistate.SSHDestination) *sshPromptState {
	ti := newFormInput("user", 40)
	ti.SetValue(prefill)
	ti.CursorEnd()
	path := newFormInput("~/.ssh/id_ed25519", 60)
	path.Blur()
	proxy := newFormInput("host:port", 40)
	proxy.Blur()

	choices := destinationChoicesFor(r, info.aliases)
	route := ""
	if remembered.Kind == "route" {
		route = remembered.Route
		proxy.SetValue(remembered.ProxyAddress)
	}
	p := &sshPromptState{
		row: r, spec: spec, userKey: userKey, identityKey: identityKey,
		input: ti, pathInput: path, proxyInput: proxy, prefill: prefill, explicit: explicit,
		remembered: spec.IdentityFile, loading: true,
		destinations: choices, destErr: info.err, route: route,
	}
	p.destCursor = initialDestCursor(choices, spec, remembered)
	p.focus = sshFocusDestination
	p.syncFocus()
	return p
}

// destinationChoicesFor lists every OpenSSH alias discovery confirmed for
// this VM, then its own native DNS name and IP — the "OpenSSH destinations"
// and "Native VM targets" groups the issue's UX section describes. An IP
// identical to the DNS name already listed is not repeated, the same rule
// sshTargets applies.
func destinationChoicesFor(r row, aliases []string) []destinationChoice {
	var out []destinationChoice
	for _, a := range aliases {
		out = append(out, destinationChoice{kind: destAlias, value: a, label: "OpenSSH: " + a})
	}
	dns := ""
	if h := r.target.hostName; h != "" && !isLoopbackName(h) {
		dns = h
		out = append(out, destinationChoice{kind: destNative, value: h, label: "Guest DNS: " + h})
	}
	if a := r.target.address; a != "" && a != dns {
		out = append(out, destinationChoice{kind: destNative, value: a, label: "Guest IP: " + a})
	}
	return out
}

// initialDestCursor preselects a remembered, still-valid alias first, else
// whichever native target the action was opened from, else the first row.
func initialDestCursor(choices []destinationChoice, spec SSHSpec, remembered uistate.SSHDestination) int {
	if remembered.Kind == "openssh_alias" {
		for i, c := range choices {
			if c.kind == destAlias && c.value == remembered.Alias {
				return i
			}
		}
	}
	for i, c := range choices {
		if c.kind == destNative && c.value == spec.Address {
			return i
		}
	}
	return 0
}

func (p *sshPromptState) destinationIsAlias() bool {
	if p.destCursor < 0 || p.destCursor >= len(p.destinations) {
		return false
	}
	return p.destinations[p.destCursor].kind == destAlias
}

func (p *sshPromptState) routeIndex() int {
	for i, r := range routeOrder {
		if r == p.route {
			return i
		}
	}
	return 0
}

// order is the focus sections the overlay currently offers, in order: an
// OpenSSH alias destination skips Route (and so ProxyAddr) entirely — see
// SSHSpec.Alias — since OpenSSH alone owns its route.
func (p *sshPromptState) order() []sshPromptFocus {
	order := []sshPromptFocus{sshFocusDestination}
	if !p.destinationIsAlias() {
		order = append(order, sshFocusRoute)
		if routeNeedsProxyAddr(p.route) {
			order = append(order, sshFocusProxyAddr)
		}
	}
	return append(order, sshFocusUser, sshFocusIdentity)
}

func (p *sshPromptState) advanceFocus(delta int) {
	order := p.order()
	idx := 0
	for i, f := range order {
		if f == p.focus {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(order)) % len(order)
	p.focus = order[idx]
	p.syncFocus()
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

// handleSSHPromptKey drives the combined overlay. Enter on a list section
// (Destination, Route, Identity) advances to the next applicable section;
// only a confirmed identity choice or manual path starts ssh(1). Every
// other key remains inside the overlay.
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
			p.advanceFocus(-1)
		} else {
			p.advanceFocus(1)
		}
		return nil
	}

	switch p.focus {
	case sshFocusDestination:
		switch msg.Type {
		case tea.KeyUp:
			p.destCursor = clamp(p.destCursor-1, 0, len(p.destinations)-1)
		case tea.KeyDown:
			p.destCursor = clamp(p.destCursor+1, 0, len(p.destinations)-1)
		case tea.KeyEnter:
			p.err = ""
			p.advanceFocus(1)
		}
		return nil

	case sshFocusRoute:
		switch msg.Type {
		case tea.KeyUp:
			p.route = routeOrder[clamp(p.routeIndex()-1, 0, len(routeOrder)-1)]
		case tea.KeyDown:
			p.route = routeOrder[clamp(p.routeIndex()+1, 0, len(routeOrder)-1)]
		case tea.KeyEnter:
			p.err = ""
			p.advanceFocus(1)
		}
		return nil

	case sshFocusProxyAddr:
		if msg.Type == tea.KeyEnter {
			addr := strings.TrimSpace(p.proxyInput.Value())
			if !config.ValidSSHProxyAddress(addr) {
				p.err = fmt.Sprintf("proxy address %q must be host:port", addr)
				return nil
			}
			p.err = ""
			p.advanceFocus(1)
			return nil
		}
		p.err = ""
		var cmd tea.Cmd
		p.proxyInput, cmd = p.proxyInput.Update(msg)
		return cmd

	case sshFocusIdentity:
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
		return nil

	case sshFocusPath:
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

	// sshFocusUser
	if msg.Type == tea.KeyEnter {
		p.err = ""
		p.advanceFocus(1)
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
	if p.focus == sshFocusProxyAddr {
		p.proxyInput.Focus()
	} else {
		p.proxyInput.Blur()
	}
	if p.focus != sshFocusPath {
		p.pathInput.Blur()
	}
}

// connectFromSSHPrompt builds the final SSHSpec from every section's
// current choice and, once every value validates, closes the overlay,
// remembers what should persist, and starts the handoff. Any validation
// failure leaves the overlay open with p.err set and focus on the section
// that needs fixing, exactly like the identity and manual-path checks it
// already performed.
func (m *Model) connectFromSSHPrompt(p *sshPromptState, identity string) tea.Cmd {
	user := strings.TrimSpace(p.input.Value())
	if user != "" && !validSSHUser(user) {
		p.err = "not a valid user name"
		p.focus = sshFocusUser
		p.syncFocus()
		return nil
	}
	if p.destCursor < 0 || p.destCursor >= len(p.destinations) {
		p.err = "choose a destination"
		p.focus = sshFocusDestination
		return nil
	}
	d := p.destinations[p.destCursor]

	var spec SSHSpec
	switch {
	case d.kind == destAlias:
		spec = SSHSpec{Address: d.value, Alias: true}
	case p.route == "":
		var reason string
		spec, reason = m.autoSSHSpec(SSHSpec{Address: d.value}, p.row)
		if reason != "" {
			p.err = reason
			return nil
		}
	default:
		proxyAddr := strings.TrimSpace(p.proxyInput.Value())
		if routeNeedsProxyAddr(p.route) && !config.ValidSSHProxyAddress(proxyAddr) {
			p.err = fmt.Sprintf("proxy address %q must be host:port", proxyAddr)
			p.focus = sshFocusProxyAddr
			p.syncFocus()
			return nil
		}
		args, reason := routeOverrideArgs(p.route, proxyAddr)
		if reason != "" {
			p.err = reason
			return nil
		}
		spec = SSHSpec{Address: d.value, ProxyArgs: args}
	}

	destKey := sshUserKey(p.row)
	m.sshPrompt = nil
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

	switch {
	case d.kind == destAlias:
		m.rememberSSHDestination(destKey, uistate.SSHDestination{Kind: "openssh_alias", Alias: d.value})
	case p.route == "":
		m.forgetSSHDestination(destKey)
	default:
		m.rememberSSHDestination(destKey, uistate.SSHDestination{
			Kind: "route", Route: p.route, ProxyAddress: strings.TrimSpace(p.proxyInput.Value()),
		})
	}
	return m.sshCmd(spec)
}

// openSSHPrompt opens the combined overlay, reusing whatever alias
// discovery has already run for this row (see sshDestInfoFor) rather than
// running "ssh -G" against every candidate a second time. Only identity
// discovery remains asynchronous, with its own spinner — Destination and
// Route need no network or process access to populate.
func (m *Model) openSSHPrompt(r row, spec SSHSpec, userKey, prefill string, explicit bool, identityKey string, info sshDestInfo) tea.Cmd {
	remembered := m.sshDestinations[sshUserKey(r)]
	p := newSSHPromptState(r, spec, userKey, prefill, explicit, identityKey, info, remembered)
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

// viewSSHPrompt renders the combined destination, route, user and identity
// chooser.
func (m *Model) viewSSHPrompt() []string {
	t := m.theme
	p := m.sshPrompt
	p.input.Width = clamp(m.width-2, 10, 40)
	p.pathInput.Width = clamp(m.width-2, 20, 70)
	p.proxyInput.Width = clamp(m.width-2, 10, 40)

	lines := []string{t.title.Render("SSH to " + p.spec.Address), ""}

	lines = append(lines, "  "+t.label.Render("Destination"))
	for i, d := range p.destinations {
		marker := "  "
		if p.focus == sshFocusDestination && i == p.destCursor {
			marker = "▸ "
		}
		lines = append(lines, marker+d.label)
	}
	if p.destErr != nil {
		lines = append(lines, "  "+t.dim.Render("Could not fully inspect ~/.ssh; showing what discovery found so far."))
	}

	if p.destinationIsAlias() {
		lines = append(lines, "", "  "+t.dim.Render("OpenSSH owns this alias's route (ProxyJump/ProxyCommand/Port)."))
	} else {
		lines = append(lines, "", "  "+t.label.Render("Route"))
		for i, kind := range routeOrder {
			marker := "  "
			if p.focus == sshFocusRoute && i == p.routeIndex() {
				marker = "▸ "
			}
			lines = append(lines, marker+routeLabel(kind))
		}
		if routeNeedsProxyAddr(p.route) {
			addrLabel := "  " + t.label.Render("Proxy address") + "  "
			if p.focus == sshFocusProxyAddr {
				addrLabel = "▸ " + t.focused.Render("Proxy address") + "  "
			}
			lines = append(lines, "", addrLabel+p.proxyInput.View())
		}
	}

	userLabel := "  " + t.label.Render("User") + "  "
	if p.focus == sshFocusUser {
		userLabel = "▸ " + t.focused.Render("User") + "  "
	}
	lines = append(lines, "", userLabel+p.input.View(), "", "  "+t.label.Render("Identity"))
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
	for _, l := range wrap("Tab switches between Destination, Route, User and Identity. Choosing an OpenSSH alias leaves ~/.ssh/config and ssh-agent fully in control of the route. Selecting OpenSSH default for User leaves ~/.ssh/config and ssh-agent in control there too; an explicit key uses public-key authentication only.", m.width-2) {
		lines = append(lines, "  "+t.dim.Render(l))
	}
	lines = append(lines, "")
	lines = append(lines, m.overlayKeys([][2]string{{"enter", "next / connect"}, {"tab", "switch field"}, {"esc", "cancel"}, {"ctrl+c", "quit"}})...)
	return scrollLines(lines, 0, m.bodyHeight())
}
