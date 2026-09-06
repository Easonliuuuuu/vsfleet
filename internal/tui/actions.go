package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

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
// modeDetail, so it takes key priority inside handleDetailKey rather than
// globally in handleKey.
type actionList struct {
	items  []action
	cursor int
}

// demoDisabledReason is why every action that would launch a process or a
// browser is disabled while Options.Demo is set — see the Handoff doc
// comment for why that check lives here instead of inside a Handoff
// implementation.
const demoDisabledReason = "demo — no external commands"

// actionsFor builds the actions available on one line of an open detail
// pane. Line 0 is the object's own header; every other line is
// r.detail[cursor-2] — see detailFocusable, whose indexing this matches
// exactly (index 1 is always the blank line under the title).
func (m *Model) actionsFor(r row, cursor int) []action {
	if cursor == 0 {
		return m.objectActions(r)
	}
	i := cursor - 2
	if i < 0 || i >= len(r.detail) {
		return nil
	}
	return m.fieldActions(r, r.detail[i])
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

// sshAction is "SSH to <address>", offered on a VM's or host's object header
// and on a VM's IP address field alike — the target and the route are the
// same question either place is asked from.
func (m *Model) sshAction(r row, address string) action {
	label := "SSH to " + address
	if address == "" {
		return action{label: "SSH", disabled: "no address available"}
	}
	if m.demo {
		return action{label: label, disabled: demoDisabledReason}
	}
	spec := SSHSpec{Address: address, User: m.sshUser}
	if st := m.byName[r.context]; st != nil {
		args, reason := proxyArgs(st.cc.Transport)
		if reason != "" {
			return action{label: label, disabled: reason}
		}
		spec.ProxyArgs = args
	}
	return action{label: label, run: func(m *Model) tea.Cmd { return m.sshCmd(spec) }}
}

// sshCommandCopyAction is "Copy ssh user@address" for a VM's IP field — the
// string an operator pastes into a second tmux pane rather than handing this
// program's own terminal over.
func (m *Model) sshCommandCopyAction(address string) action {
	target := address
	if m.sshUser != "" {
		target = m.sshUser + "@" + address
	}
	cmd := "ssh " + target
	return action{label: "Copy " + cmd, run: func(m *Model) tea.Cmd { return m.copyCmd(cmd) }}
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
		m.mode = m.detailFrom
		return nil
	}}
}

func (m *Model) objectActions(r row) []action {
	var out []action
	switch r.kind {
	case vsphere.KindVM:
		if r.target.address != "" {
			out = append(out, m.sshAction(r, r.target.address))
		}
		out = append(out, m.openAction(r))
		out = append(out, copyNamed("Copy MoRef", r.target.moref))
	case vsphere.KindTemplate:
		out = append(out, m.openAction(r))
		out = append(out, copyNamed("Copy MoRef", r.target.moref))
	case vsphere.KindHost:
		out = append(out, m.sshAction(r, r.target.address))
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
		return []action{m.sshAction(r, f.value), m.sshCommandCopyAction(f.value), copyAction(f.value)}
	case r.kind == vsphere.KindVM && f.label == "Host":
		return []action{jumpToNamed("Show this host", vsphere.KindHost, f.value), copyAction(f.value)}
	case r.kind == vsphere.KindVM && f.label == "Cluster":
		return []action{jumpToNamed("Show this cluster", vsphere.KindCluster, f.value), copyAction(f.value)}
	case f.label == "Managed object":
		return []action{copyNamed("Copy MoRef", f.value)}
	default:
		return []action{copyAction(f.value)}
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
// does nothing.
func (m *Model) openFieldActions() tea.Cmd {
	r, ok := m.currentRow()
	if !ok {
		return nil
	}
	items := m.actionsFor(r, m.detailCursor)
	switch {
	case len(items) == 0:
		return nil
	case len(items) == 1:
		return m.runAction(items[0])
	default:
		m.actions = &actionList{items: items}
		return nil
	}
}

// handleActionsKey drives the open popup: it owns every key ahead of the
// pane underneath it, the same priority credPrompt holds globally — except
// this overlay is reachable only from modeDetail, so the check lives at the
// top of handleDetailKey instead of handleKey.
func (m *Model) handleActionsKey(msg tea.KeyMsg) tea.Cmd {
	al := m.actions
	switch {
	case key.Matches(msg, m.keys.Back):
		m.actions = nil
	case key.Matches(msg, m.keys.Up):
		al.cursor = clamp(al.cursor-1, 0, len(al.items)-1)
	case key.Matches(msg, m.keys.Down):
		al.cursor = clamp(al.cursor+1, 0, len(al.items)-1)
	case key.Matches(msg, m.keys.Open):
		if al.cursor >= 0 && al.cursor < len(al.items) && al.items[al.cursor].disabled == "" {
			return m.runAction(al.items[al.cursor])
		}
	}
	return nil
}
