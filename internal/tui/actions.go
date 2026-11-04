package tui

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// action is one entry in the field cursor's action list: what to call it, an
// optional second column showing the command or URL it would actually run,
// why it cannot run right now (empty means it can), and what running it
// does. disabled and run are mutually exclusive in practice — an action with
// a reason never has a run func — but nothing enforces that beyond runAction
// checking disabled first.
type action struct {
	label    string
	detail   string
	disabled string
	run      func(m *Model) tea.Cmd
}

// actionList is the popup opened on a line with more than one action,
// non-nil on Model exactly while it is open — the same idiom credPrompt
// already uses. Unlike credPrompt it is only ever reachable from
// modeDetail or modeVAppVMDetail, so it takes key priority inside their
// handlers rather than globally in handleKey.
type actionList struct {
	items  []action
	cursor int
}

// demoDisabledReason is why every action that would launch a process or a
// browser is disabled while Options.Demo is set — see the Handoff doc
// comment for why that check lives here instead of inside a Handoff
// implementation.
const demoDisabledReason = "demo — no external commands"

// demoReadOnlyReason is why actions that mutate contexts or persistent
// configuration are disabled in demo mode. The presentation is read-only.
const demoReadOnlyReason = "demo — read-only"

// actionsFor builds the actions available on one line of an open detail
// pane. Line 0 is the object's own header; every other line is
// r.detail[cursor-2] — see detailFocusable, whose indexing this matches
// exactly (index 1 is always the blank line under the title).
//
// Whatever the builders return, runnable actions come first and actions that
// cannot run right now follow in their original order, so the popup opens on
// something Enter can do and the unavailable entries read as a footnote. An
// action that does not apply to the object at all is not built in the first
// place; see addContextAction for the distinction.
func (m *Model) actionsFor(r row, cursor int) []action {
	if cursor == 0 {
		return runnableFirst(m.objectActions(r))
	}
	i := cursor - 2
	if i < 0 || i >= len(r.detail) {
		return nil
	}
	return runnableFirst(m.fieldActions(r, r.detail[i]))
}

// runnable reports whether Enter on a can do anything.
func (a action) runnable() bool { return a.disabled == "" && a.run != nil }

// runnableFirst stably moves actions that cannot run behind those that can.
func runnableFirst(items []action) []action {
	slices.SortStableFunc(items, func(a, b action) int {
		switch {
		case a.runnable() == b.runnable():
			return 0
		case a.runnable():
			return -1
		default:
			return 1
		}
	})
	return items
}

// firstRunnable is the index of the first action Enter can run, or -1.
func firstRunnable(items []action) int {
	return slices.IndexFunc(items, action.runnable)
}

// copyAction is "Copy value" — the one action every addressable line offers,
// which every other action sits alongside rather than replaces.
func copyAction(value string) action {
	return action{label: "Copy value", run: func(m *Model) tea.Cmd { return m.copyCmd(value) }}
}

func copyNamed(label, value string) action {
	return action{label: label, run: func(m *Model) tea.Cmd { return m.copyCmd(value) }}
}

// openAction is "Open in vSphere Client" for whatever row it is given,
// built once and shared by the object header of every kind that has a deep
// link. It reports its own unavailability — no server GUID yet, or a route
// the browser cannot follow — as a disabled reason rather than being
// omitted, so an operator sees why rather than wondering where it went.
func (m *Model) openAction(r row) action {
	st := m.byName[r.context]
	if st == nil {
		return action{label: "Open in vSphere Client", disabled: "vCenter not in scope"}
	}
	if m.demo {
		return action{label: "Open in vSphere Client", disabled: demoDisabledReason}
	}
	statusInfo, _ := m.backend.Status(r.context)
	url, ok := vsphereClientURL(st.cc, r.target.morefKind, r.target.moref, statusInfo.InstanceID)
	if !ok {
		return action{label: "Open in vSphere Client", disabled: "vCenter identity not known yet — reload and try again"}
	}
	if !isDirect(st.cc.Transport) {
		return action{
			label:    "Open in vSphere Client",
			disabled: "no route — " + r.context + " is proxied",
			run:      nil,
		}
	}
	return action{label: "Open in vSphere Client", detail: url, run: func(m *Model) tea.Cmd { return m.openURLCmd(url) }}
}

// hostClientAction is "Open in Host Client", offered only for hosts. It
// needs no vCenter identity, only the host's own address, but the browser
// still cannot reach it through a proxy any more than it can reach the
// vSphere Client — so the same disabled reason applies.
func (m *Model) hostClientAction(r row) action {
	url, ok := hostClientURL(r.target.address)
	if !ok {
		return action{label: "Open in Host Client", disabled: "no address for this host"}
	}
	if m.demo {
		return action{label: "Open in Host Client", disabled: demoDisabledReason}
	}
	if st := m.byName[r.context]; st != nil && !isDirect(st.cc.Transport) {
		return action{label: "Open in Host Client", disabled: "no route — " + r.context + " is proxied"}
	}
	return action{label: "Open in Host Client", detail: url, run: func(m *Model) tea.Cmd { return m.openURLCmd(url) }}
}

// sshAction is "SSH to <address>", run as plain "ssh <address>". vSphere
// says which addresses a machine has, not which one this workstation can
// reach or through what, so vsfleet adds no user, key or route: the
// operator's ~/.ssh/config decides those. note, when set, says where the
// address came from, such as the network its adapter is on.
func (m *Model) sshAction(address, note string) action {
	if address == "" {
		return action{label: "SSH", disabled: "no address available"}
	}
	label := "SSH to " + address
	if note != "" {
		label += " · " + note
	}
	if m.demo {
		return action{label: label, disabled: demoDisabledReason}
	}
	return action{label: label, detail: sshCommand(address), run: func(m *Model) tea.Cmd { return m.sshCmd(address) }}
}

// sshDestination is one address a VM can be reached at, and where it came
// from.
type sshDestination struct {
	address string
	note    string
}

// vmSSHDestinations lists every address a VM reports, name first: the guest
// DNS name, then the primary IP, then each other address on its adapters,
// labelled with the adapter's network. Which one is reachable from here is
// something only the operator knows, so all of them are offered. Adapter
// addresses come from a capture; a live listing knows only the primary IP.
func vmSSHDestinations(r row) []sshDestination {
	var out []sshDestination
	seen := map[string]bool{}
	add := func(address, note string) {
		if address == "" || seen[address] {
			return
		}
		seen[address] = true
		out = append(out, sshDestination{address: address, note: note})
	}
	if h := r.target.hostName; h != "" && !isLoopbackName(h) {
		add(h, "guest DNS name")
	}
	var nics []vsphere.VMNIC
	if r.vm != nil {
		nics = r.vm.NICs
	}
	primaryNote := "primary IP"
	for _, n := range nics {
		if slices.Contains(n.IPv4, r.target.address) || slices.Contains(n.IPv6, r.target.address) {
			if n.Network != "" {
				primaryNote += ", " + n.Network
			}
			break
		}
	}
	add(r.target.address, primaryNote)
	for _, n := range nics {
		for _, a := range append(append([]string(nil), n.IPv4...), n.IPv6...) {
			if ip, err := netip.ParseAddr(a); err == nil && ip.IsLinkLocalUnicast() {
				continue
			}
			add(a, n.Network)
		}
	}
	return out
}

// isLoopbackName spots the name an unconfigured guest reports for itself,
// which would send ssh to the operator's own machine.
func isLoopbackName(name string) bool {
	name = strings.ToLower(name)
	return name == "localhost" || strings.HasPrefix(name, "localhost.")
}

// vmSSHActions is one SSH entry per address the VM reports. The notes only
// help to tell several addresses apart, so a single one goes without.
func (m *Model) vmSSHActions(r row) []action {
	dests := vmSSHDestinations(r)
	switch len(dests) {
	case 0:
		return []action{m.sshAction("", "")}
	case 1:
		return []action{m.sshAction(dests[0].address, "")}
	}
	out := make([]action, 0, len(dests))
	for _, d := range dests {
		out = append(out, m.sshAction(d.address, d.note))
	}
	return out
}

func sshCommand(address string) string {
	return "ssh " + shellQuote(address)
}

func shellQuote(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_@%+=:,./-", r))
	}) < 0 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// nestedContextFor finds the context represented by a VM in the parent
// context. Saved provenance is authoritative because a VM's IP can change;
// the host-name fallback keeps hand-made contexts and older files useful.
func (m *Model) nestedContextFor(parentContext, moref, address string) *contextState {
	for _, st := range m.states {
		if st.cc.Via == parentContext && st.cc.ViaMoRef == moref {
			return st
		}
	}
	if address == "" {
		return nil
	}
	for _, st := range m.states {
		if st.cc.Name == parentContext {
			continue
		}
		if st.cc.Host() == address {
			return st
		}
	}
	return nil
}

// vcenterNameEvidence reports whether a VM's own name, or the name its guest
// reports, reads as a vCenter Server: a name part that starts with "vcsa",
// "vcenter" or "vcentre", or is "vc" or "vc" plus digits ("vc01"). Parts are
// split on anything that is not a letter or digit, so "prod-vcsa-01" and
// "VCenter.lab.local" match while "vcls-1" (a vSphere cluster service agent),
// "abc01" and "dc-01" do not. It is deliberately name evidence only: the guest
// OS cannot tell a vCenter appliance from any other Photon OS appliance.
func vcenterNameEvidence(names ...string) bool {
	for _, name := range names {
		parts := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})
		for _, p := range parts {
			switch {
			case strings.HasPrefix(p, "vcsa"), strings.HasPrefix(p, "vcenter"), strings.HasPrefix(p, "vcentre"):
				return true
			case p == "vc":
				return true
			case strings.HasPrefix(p, "vc") && strings.Trim(p[2:], "0123456789") == "":
				return true
			}
		}
	}
	return false
}

// addContextAction promotes a VM that plausibly is a vCenter to a context
// form, or switches to the context it already is. The second result says
// whether the action applies to this VM at all; a VM that does not look like
// a vCenter (a domain controller, a build runner) is not offered it, instead
// of being offered a disabled entry that can never become runnable. What does
// apply but cannot run right now stays visible with its reason: no address
// yet, or demo mode, where the presentation is read-only and does not persist
// context mutations. Existing nested contexts remain switchable and are always
// offered, since saved provenance or an address match already proves the VM
// is a vCenter the operator has added.
func (m *Model) addContextAction(r row, address string) (action, bool) {
	if nested := m.nestedContextFor(r.context, r.target.moref, address); nested != nil {
		name := nested.cc.Name
		return action{
			label: fmt.Sprintf("Switch to context %q", name),
			run: func(m *Model) tea.Cmd {
				m.selectByName(name)
				m.allScope = false
				m.cursor, m.offset = 0, 0
				m.setMessage("", false)
				m.mode = modeBrowse
				return tea.Batch(m.ensureSelectedLoaded(false)...)
			},
		}, true
	}

	if !vcenterNameEvidence(r.name, r.target.hostName) {
		return action{}, false
	}
	slug := m.uniqueContextName(r.name)
	label := fmt.Sprintf("Add %q as a vCenter context", slug)
	if address == "" {
		return action{label: label, disabled: "no address available"}, true
	}
	if m.demo {
		return action{label: label, disabled: demoReadOnlyReason}, true
	}
	transport := config.TransportConfig{Type: config.TransportDirect}
	if parent := m.byName[r.context]; parent != nil {
		transport = parent.cc.Transport
	}
	seed := contextSeed{
		name:      slug,
		endpoint:  "https://" + address,
		transport: transport,
		via:       r.context,
		viaMoRef:  r.target.moref,
	}
	return action{label: label, run: func(m *Model) tea.Cmd {
		m.returnTo = m.mode
		return m.enterFormSeeded(seed)
	}}, true
}

// jumpAction narrows the table to kind's rows whose actionJoins field named
// by matcher equals value — "the VMs on this host" — until Esc clears it.
// See jumpConstraint and its use in Model.rows.
func jumpAction(label string, kind vsphere.Kind, matcher, value string) action {
	return action{label: label, run: func(m *Model) tea.Cmd {
		m.jump = &jumpConstraint{kind: kind, matcher: matcher, value: value, label: label}
		m.kind = kind
		m.filter.SetValue("")
		m.cursor, m.offset = 0, 0
		m.clearVAppWorkspace()
		m.clearSwitchWorkspace()
		m.clearDSWorkspace()
		m.mode = m.detailFrom
		return nil
	}}
}

// jumpToNamed switches to kind's tab filtered to name by the ordinary text
// filter — the simple direction of a cross-resource jump, where the target
// is one specific object named on the row already ("this VM's Host field"
// pointing at that host), as opposed to jumpAction's "everything joined to
// this row", which the name filter cannot express.
func jumpToNamed(label string, kind vsphere.Kind, name string) action {
	return action{label: label, run: func(m *Model) tea.Cmd {
		m.jump = nil
		m.kind = kind
		m.filter.SetValue(name)
		m.cursor, m.offset = 0, 0
		m.clearVAppWorkspace()
		m.clearSwitchWorkspace()
		m.clearDSWorkspace()
		m.mode = m.detailFrom
		return nil
	}}
}

func (m *Model) objectActions(r row) []action {
	var out []action
	switch r.kind {
	case vsphere.KindVM:
		out = append(out, m.vmSSHActions(r)...)
		if a, ok := m.addContextAction(r, r.target.address); ok {
			out = append(out, a)
		}
		out = append(out, m.openAction(r))
		out = append(out, copyNamed("Copy MoRef", r.target.moref))
	case vsphere.KindTemplate:
		out = append(out, m.openAction(r))
		out = append(out, copyNamed("Copy MoRef", r.target.moref))
	case vsphere.KindHost:
		out = append(out, m.sshAction(r.target.address, ""))
		out = append(out, m.hostClientAction(r))
		out = append(out, m.openAction(r))
		out = append(out, jumpAction("Show VMs on this host", vsphere.KindVM, "host", r.name))
		out = append(out, copyNamed("Copy MoRef", r.target.moref))
	case vsphere.KindCluster:
		out = append(out, m.openAction(r))
		out = append(out, jumpAction("Show hosts in cluster", vsphere.KindHost, "cluster", r.name))
		out = append(out, jumpAction("Show VMs in cluster", vsphere.KindVM, "cluster", r.name))
		out = append(out, copyNamed("Copy MoRef", r.target.moref))
	case vsphere.KindDatastore:
		// Browsing comes first: it is the thing an operator opened a
		// datastore to do that they could not do here before.
		out = append(out, m.browseFilesAction(r))
		out = append(out, m.findFilesAction(r))
		out = append(out, m.openAction(r))
		out = append(out, jumpAction("Show VMs on this datastore", vsphere.KindVM, "datastore", r.name))
		if r.target.path != "" {
			out = append(out, copyNamed("Copy datastore path", r.target.path))
		}
		out = append(out, copyNamed("Copy MoRef", r.target.moref))
	case vsphere.KindNetwork:
		out = append(out, m.openAction(r))
		out = append(out, jumpAction("Show VMs on this network", vsphere.KindVM, "network", r.name))
		out = append(out, copyNamed("Copy MoRef", r.target.moref))
	case vsphere.KindVApp:
		out = append(out, m.openAction(r))
		out = append(out, copyNamed("Copy MoRef", r.target.moref))
	}
	out = append(out, copyAction(r.name))
	return out
}

func (m *Model) fieldActions(r row, f field) []action {
	switch {
	case r.kind == vsphere.KindVM && f.label == "IP address":
		return m.sshFieldActions(f.value)
	case r.kind == vsphere.KindVM && f.label == "DNS name":
		return m.sshFieldActions(f.value)
	case r.kind == vsphere.KindVM && f.label == "Host":
		return []action{jumpToNamed("Show this host", vsphere.KindHost, f.value), copyAction(f.value)}
	case r.kind == vsphere.KindVM && f.label == "Cluster":
		return []action{jumpToNamed("Show this cluster", vsphere.KindCluster, f.value), copyAction(f.value)}
	case r.cluster != nil && !r.cluster.Standalone && f.label == "VMs":
		return []action{jumpAction("Show VMs in cluster", vsphere.KindVM, "cluster", r.name), copyAction(f.value)}
	case r.cluster != nil && !r.cluster.Standalone && (f.label == "Hosts" || f.label == "Host states"):
		return []action{jumpAction("Show hosts in cluster", vsphere.KindHost, "cluster", r.name), copyAction(f.value)}
	case r.cluster != nil && f.label == "Datastores":
		return []action{{label: "Open the Storage page", run: func(m *Model) tea.Cmd {
			m.clusterState(r).page = 2
			return nil
		}}, copyAction(f.value)}
	case f.label == "Managed object":
		return []action{copyNamed("Copy MoRef", f.value)}
	default:
		return []action{copyAction(f.value)}
	}
}

// sshFieldActions is what a VM's IP address or DNS name line offers: SSH to
// that address, or copy the command or the address to use elsewhere.
func (m *Model) sshFieldActions(address string) []action {
	cmd := sshCommand(address)
	return []action{
		m.sshAction(address, ""),
		{label: "Copy " + cmd, run: func(m *Model) tea.Cmd { return m.copyCmd(cmd) }},
		copyAction(address),
	}
}

// runAction closes the popup (if one was open) and, unless the action
// declined to be runnable, runs it. A disabled action reaching here at all
// would be a bug in how the list was navigated, not a state runAction needs
// to explain — the popup never lets the cursor land on one; see
// actionListLines.
func (m *Model) runAction(a action) tea.Cmd {
	m.actions = nil
	if a.disabled != "" || a.run == nil {
		return nil
	}
	return a.run(m)
}

// openFieldActions is Enter's meaning inside a detail pane: collect the
// focused line's actions, run the one action immediately if there is
// exactly one, or open the popup to choose among several. A line with no
// actions at all (there is always at least "Copy value", so this is
// unreachable today, but a future field type might have nothing to copy)
// does nothing. The popup's cursor starts on the first runnable action, and
// when nothing on the line can run, Enter says why in the message line
// instead of opening a list nobody can choose from.
func (m *Model) openFieldActions() tea.Cmd {
	r, ok := m.detailRow()
	if !ok {
		return nil
	}
	return m.openActionList(m.actionsFor(r, m.detailCursor))
}

// openActionList acts on items, already ordered runnable-first: nothing for
// none, the action itself for exactly one, a message when none can run, else
// the popup with its cursor on the first runnable entry.
func (m *Model) openActionList(items []action) tea.Cmd {
	first := firstRunnable(items)
	switch {
	case len(items) == 0:
		return nil
	case first < 0:
		a := items[0]
		m.setMessage(fmt.Sprintf("Nothing to run here — %s: %s", a.label, a.disabled), true)
		return nil
	case len(items) == 1:
		return m.runAction(items[0])
	default:
		m.actions = &actionList{items: items, cursor: first}
		m.scrollVMActionIntoView()
		return nil
	}
}

// move steps the popup cursor by dir to the next runnable action, staying
// put when there is none that way: a disabled entry is shown for its reason,
// never selectable.
func (al *actionList) move(dir int) {
	for i := al.cursor + dir; i >= 0 && i < len(al.items); i += dir {
		if al.items[i].runnable() {
			al.cursor = i
			return
		}
	}
}

// handleActionsKey drives the open popup: it owns every key ahead of the
// pane underneath it, the same priority credPrompt holds globally — except
// this overlay is reachable only from modeDetail or modeVAppVMDetail, so the
// check lives at the top of those handlers instead of handleKey.
func (m *Model) handleActionsKey(msg tea.KeyMsg) tea.Cmd {
	al := m.actions
	switch {
	case key.Matches(msg, m.keys.Back):
		m.actions = nil
	case key.Matches(msg, m.keys.Up):
		al.move(-1)
	case key.Matches(msg, m.keys.Down):
		al.move(1)
	case key.Matches(msg, m.keys.Open):
		if al.cursor >= 0 && al.cursor < len(al.items) && al.items[al.cursor].runnable() {
			return m.runAction(al.items[al.cursor])
		}
	}
	m.scrollVMActionIntoView()
	return nil
}
