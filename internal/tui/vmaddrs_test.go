package tui

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// addrFakeBackend answers the VM pane's address read with nics, or err.
type addrFakeBackend struct {
	*fakeBackend
	nics  []vsphere.VMNIC
	err   error
	calls []string
}

func (b *addrFakeBackend) VMGuestNICs(_ context.Context, _ *config.Context, vmID string) ([]vsphere.VMNIC, error) {
	b.calls = append(b.calls, vmID)
	return b.nics, b.err
}

func addrModel(t *testing.T, nics []vsphere.VMNIC) (*Model, *addrFakeBackend) {
	t.Helper()
	b := &addrFakeBackend{fakeBackend: twoHealthy(), nics: nics}
	m := newTestModel(t, b.fakeBackend, Options{Current: "prod"})
	m.backend = b
	return m, b
}

func detailValue(r row, label string) (string, bool) {
	for _, f := range r.detail {
		if f.label == label {
			return f.value, true
		}
	}
	return "", false
}

// Opening a VM from a live listing reads its adapters once, then shows the
// addresses besides the primary under "IP address" and offers SSH to each.
func TestOpeningAVMShowsItsOtherAddresses(t *testing.T) {
	m, b := addrModel(t, nil)
	r := findRow(t, m, vsphere.KindVM, "app-01")
	b.nics = []vsphere.VMNIC{
		{Network: "VM Network", IPv4: []string{r.target.address}},
		{Network: "backup", IPv4: []string{"192.168.40.5"}},
	}
	press(t, m, "enter")

	if !slices.Equal(b.calls, []string{r.vm.ID}) {
		t.Fatalf("address reads = %v, want one for %s", b.calls, r.vm.ID)
	}
	open, _ := m.detailRow()
	if got, _ := detailValue(open, "Other IPs"); got != "192.168.40.5 (backup)" {
		t.Errorf("Other IPs = %q, want the backup address with its network", got)
	}
	if _, ok := findAction(m.actionsFor(open, 0), "SSH to 192.168.40.5 · backup"); !ok {
		t.Errorf("no SSH to the second address: %v", labels(m.actionsFor(open, 0)))
	}

	press(t, m, "esc", "enter")
	if len(b.calls) != 1 {
		t.Errorf("reopening the VM read its addresses again: %v", b.calls)
	}
}

// A VM with only its primary address, or one whose read failed, shows no
// "Other IPs" line: the pane looks as it did before.
func TestNoOtherAddressesAddsNoLine(t *testing.T) {
	for _, tc := range []struct {
		name string
		nics []vsphere.VMNIC
		err  error
	}{
		{name: "primary only", nics: []vsphere.VMNIC{{Network: "VM Network", IPv4: []string{"10.20.0.11"}}}},
		{name: "read failed", err: errors.New("session lost")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, b := addrModel(t, tc.nics)
			b.err = tc.err
			press(t, m, "enter")
			open, _ := m.detailRow()
			if v, ok := detailValue(open, "Other IPs"); ok {
				t.Errorf("unexpected Other IPs line %q", v)
			}
		})
	}
}

// A VM whose row already has its adapters, as one from a capture does, is
// not read again.
func TestAVMWithAdaptersIsNotRead(t *testing.T) {
	b := &addrFakeBackend{fakeBackend: twoHealthy()}
	b.inventories["prod"].VMs[0].NICs = []vsphere.VMNIC{{Network: "VM Network", IPv4: []string{"10.20.0.11"}}}
	m := newTestModel(t, b.fakeBackend, Options{Current: "prod"})
	m.backend = b
	if r := findRow(t, m, vsphere.KindVM, b.inventories["prod"].VMs[0].Name); len(r.vm.NICs) == 0 {
		t.Fatal("the row lost its adapters")
	}
	m.kind = vsphere.KindVM
	m.cursor = 0
	press(t, m, "enter")
	if len(b.calls) != 0 {
		t.Errorf("read the addresses of a VM that had them: %v", b.calls)
	}
}
