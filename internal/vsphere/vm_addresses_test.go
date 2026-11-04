package vsphere

import (
	"context"
	"reflect"
	"testing"

	"github.com/vmware/govmomi/vim25/types"
)

// Adapters keep every address the guest reports on them, sorted, IPv4 and
// IPv6 apart. A guest interface with no virtual adapter behind it is left
// out, the same as a capture leaves it out.
func TestGuestNICsKeepsAdapterAddresses(t *testing.T) {
	got := guestNICs([]types.GuestNicInfo{
		{DeviceConfigId: 4000, Network: "VM Network", MacAddress: "00:50:56:aa:bb:01", IpAddress: []string{"10.0.0.5", "fe80::1"}},
		{DeviceConfigId: 4001, Network: "backup", MacAddress: "00:50:56:aa:bb:02", IpAddress: []string{"192.168.40.6", "192.168.40.5", "2001:db8::5"}},
		{DeviceConfigId: -1, Network: "", IpAddress: []string{"172.17.0.1"}},
	})
	connected := false
	want := []VMNIC{
		{Network: "VM Network", MACAddress: "00:50:56:aa:bb:01", Connected: &connected, IPv4: []string{"10.0.0.5"}, IPv6: []string{"fe80::1"}},
		{Network: "backup", MACAddress: "00:50:56:aa:bb:02", Connected: &connected, IPv4: []string{"192.168.40.5", "192.168.40.6"}, IPv6: []string{"2001:db8::5"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("guestNICs() =\n%+v\nwant\n%+v", got, want)
	}
}

// The read names one VM by its managed object reference and fails for one
// that does not exist.
func TestVMGuestNICsReadsOneVM(t *testing.T) {
	c := simulatorClient(t, nil)
	vms, err := c.ListVMs(context.Background())
	if err != nil || len(vms) == 0 {
		t.Fatalf("ListVMs: %d VMs, %v", len(vms), err)
	}
	if _, err := c.VMGuestNICs(context.Background(), vms[0].ID); err != nil {
		t.Fatalf("VMGuestNICs(%s): %v", vms[0].ID, err)
	}
	if _, err := c.VMGuestNICs(context.Background(), "vm-does-not-exist"); err == nil {
		t.Error("VMGuestNICs of a missing VM returned no error")
	}
}
