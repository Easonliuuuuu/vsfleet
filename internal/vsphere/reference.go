package vsphere

import (
	"context"
	"fmt"
	"strings"

	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

// NetworkDeleted reports whether the network or distributed port group with
// managed-object id (network-N or dvportgroup-N) no longer exists.
//
// An object hidden by a NoAccess override and an object that was deleted both
// drop out of a container view, so a listing alone cannot tell them apart. A
// direct read of the object can: vCenter answers ManagedObjectNotFound only
// for an object that is gone. Any other answer, including NoPermission or a
// successful read, is false, because the object may still exist.
func (c *Client) NetworkDeleted(ctx context.Context, id string) (bool, error) {
	ref := types.ManagedObjectReference{Type: "Network", Value: id}
	if strings.HasPrefix(id, "dvportgroup-") {
		ref.Type = "DistributedVirtualPortgroup"
	}
	var content []types.ObjectContent
	err := property.DefaultCollector(c.VIM()).Retrieve(ctx, []types.ManagedObjectReference{ref}, []string{"name"}, &content)
	switch {
	case err == nil:
		return false, nil
	case isNotFound(err):
		return true, nil
	default:
		return false, fmt.Errorf("read %s: %w", id, err)
	}
}

// VMNetworkBackings returns every VM and template with only its name and NICs
// read: what a network listing is checked against, without the cost of a VM
// listing.
func (c *Client) VMNetworkBackings(ctx context.Context) ([]VM, error) {
	root, _, err := resolveRoot(ctx, c)
	if err != nil {
		return nil, err
	}
	var raw []mo.VirtualMachine
	if err := retrieve(ctx, c, root, []string{"VirtualMachine"}, []string{"VirtualMachine"}, []string{"name", "config.hardware.device"}, &raw); err != nil {
		return nil, err
	}
	out := make([]VM, 0, len(raw))
	for i := range raw {
		vm := VM{Name: raw[i].Name}
		if raw[i].Config != nil {
			_, vm.NICs, _, _ = walkDevices(raw[i].Config.Hardware.Device, nil, nil)
		}
		out = append(out, vm)
	}
	return out, nil
}

func isNotFound(err error) bool {
	switch vimFault(err).(type) {
	case types.ManagedObjectNotFound, *types.ManagedObjectNotFound:
		return true
	}
	return false
}

// vimFault returns the vSphere fault err carries, or nil when it carries none.
func vimFault(err error) types.AnyType {
	switch {
	case soap.IsSoapFault(err):
		return soap.ToSoapFault(err).VimFault()
	case soap.IsVimFault(err):
		return soap.ToVimFault(err)
	}
	return nil
}
