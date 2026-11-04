package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// vmAddressBackend reads the addresses on one VM's adapters. A live listing
// reads VMs at summary detail, which has the primary IP only, so the VM pane
// asks for the rest of the VM it has open. Optional and type-asserted like
// vmPerfBackend: without it the pane shows what the listing has.
type vmAddressBackend interface {
	VMGuestNICs(ctx context.Context, cc *config.Context, vmID string) ([]vsphere.VMNIC, error)
}

// vmAddrEntry is one VM's adapter addresses, or the read under way for them.
// gen drops a reply that a later read has superseded.
type vmAddrEntry struct {
	nics    []vsphere.VMNIC
	loading bool
	gen     uint64
}

type vmAddrsMsg struct {
	key  string
	gen  uint64
	nics []vsphere.VMNIC
	err  error
}

// loadVMAddresses reads the open VM's adapter addresses when the row does
// not already have them, as a VM from a capture does. It reads again on
// force (the refresh key) and otherwise once per VM per session: addresses
// change rarely, and opening the pane should not wait on them.
func (m *Model) loadVMAddresses(force bool) tea.Cmd {
	if m.mode != modeDetail && m.mode != modeVAppVMDetail {
		return nil
	}
	r, ok := m.detailRow()
	if !ok || r.vm == nil || r.vm.ID == "" {
		return nil
	}
	if m.vmAddrs[r.key] == nil && len(r.vm.NICs) > 0 {
		return nil // the row came with its adapters, as one from a capture does
	}
	b, ok := m.backend.(vmAddressBackend)
	if !ok {
		return nil
	}
	st, ok := m.byName[r.context]
	if !ok || st.cc == nil {
		return nil
	}
	e := m.vmAddrs[r.key]
	if e != nil && (e.loading || !force) {
		return nil
	}
	if m.vmAddrs == nil {
		m.vmAddrs = map[string]*vmAddrEntry{}
	}
	if e == nil {
		e = &vmAddrEntry{}
		m.vmAddrs[r.key] = e
	}
	m.vmAddrGen++
	e.gen, e.loading = m.vmAddrGen, true
	key, gen, ctx, cc, id := r.key, e.gen, m.ctx, st.cc, r.vm.ID
	return func() tea.Msg {
		nics, err := b.VMGuestNICs(ctx, cc, id)
		return vmAddrsMsg{key: key, gen: gen, nics: nics, err: err}
	}
}

// applyVMAddresses stores a read. A failed read keeps what an earlier one
// found: the pane then shows the primary IP, as it would without this read.
func (m *Model) applyVMAddresses(msg vmAddrsMsg) {
	e := m.vmAddrs[msg.key]
	if e == nil || e.gen != msg.gen {
		return
	}
	e.loading = false
	if msg.err == nil {
		e.nics = msg.nics
	}
}

// withVMAddresses gives a VM row the adapter addresses read for it, and an
// "Other IPs" line under "IP address" listing every address besides the
// primary, each with its network.
func (m *Model) withVMAddresses(r row) row {
	if r.vm == nil {
		return r
	}
	if e := m.vmAddrs[r.key]; e != nil && len(e.nics) > 0 {
		vm := *r.vm
		vm.NICs = e.nics
		r.vm = &vm
	}
	var others []string
	for _, d := range vmSSHDestinations(r) {
		if d.address == r.target.hostName || d.address == r.target.address {
			continue
		}
		if d.note != "" {
			d.address += " (" + d.note + ")"
		}
		others = append(others, d.address)
	}
	if len(others) == 0 {
		return r
	}
	at := len(r.detail)
	for i, f := range r.detail {
		if f.label == "IP address" {
			at = i + 1
			break
		}
	}
	detail := make([]field, 0, len(r.detail)+1)
	detail = append(detail, r.detail[:at]...)
	detail = append(detail, field{"Other IPs", strings.Join(others, ", ")})
	r.detail = append(detail, r.detail[at:]...)
	return r
}
