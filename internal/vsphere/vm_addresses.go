package vsphere

import (
	"context"
	"fmt"

	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// VMGuestNICs reads the addresses VMware Tools reports on one VM's network
// adapters. It retrieves only guest.net, so the interface can show every
// address of the VM it has open without paying for the whole estate's
// device inventory; see DetailSummary. The adapters it returns carry the
// network, MAC address, connection state and addresses, but no device label
// or backing, which only the device inventory has.
func (c *Client) VMGuestNICs(ctx context.Context, vmID string) ([]VMNIC, error) {
	var vm mo.VirtualMachine
	ref := types.ManagedObjectReference{Type: "VirtualMachine", Value: vmID}
	if err := property.DefaultCollector(c.vim.Client).RetrieveOne(ctx, ref, []string{"guest.net"}, &vm); err != nil {
		return nil, fmt.Errorf("read guest network of %s: %w", vmID, err)
	}
	if vm.Guest == nil {
		return nil, nil
	}
	return guestNICs(vm.Guest.Net), nil
}

// guestNICs maps guest.net to adapters. An interface the guest has with no
// virtual adapter behind it, such as a container bridge or a VPN tunnel,
// reports DeviceConfigId -1 and is left out, as a capture leaves it out.
func guestNICs(net []types.GuestNicInfo) []VMNIC {
	var out []VMNIC
	for _, nic := range net {
		if nic.DeviceConfigId < 0 {
			continue
		}
		var n VMNIC
		applyGuestNIC(&n, nic)
		out = append(out, n)
	}
	return out
}
