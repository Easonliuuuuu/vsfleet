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

// HostReferences returns what a host listing is checked against, reading only
// the properties that check needs: each compute resource with its host count,
// and every VM and template with its power state and the name of its host. A
// VM on a host the account cannot read has no host name, as in a VM listing.
func (c *Client) HostReferences(ctx context.Context) ([]Cluster, []VM, error) {
	root, _, err := resolveRoot(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	var computes []mo.ComputeResource
	if err := retrieve(ctx, c, root, computeResourceKinds, []string{"ComputeResource"}, []string{"name", "summary", "host"}, &computes); err != nil {
		return nil, nil, err
	}
	var hosts []mo.HostSystem
	if err := retrieve(ctx, c, root, []string{"HostSystem"}, []string{"HostSystem"}, []string{"name"}, &hosts); err != nil {
		return nil, nil, err
	}
	var raw []mo.VirtualMachine
	if err := retrieve(ctx, c, root, []string{"VirtualMachine"}, []string{"VirtualMachine"}, []string{"name", "runtime.powerState", "runtime.host"}, &raw); err != nil {
		return nil, nil, err
	}
	clusters := make([]Cluster, 0, len(computes))
	for i := range computes {
		m := &computes[i]
		cl := Cluster{Name: m.Name, Standalone: m.Self.Type != "ClusterComputeResource", Hosts: len(m.Host)}
		applyComputeSummary(&cl, m.Summary)
		clusters = append(clusters, cl)
	}
	hostNames := make(map[types.ManagedObjectReference]string, len(hosts))
	for i := range hosts {
		hostNames[hosts[i].Self] = hosts[i].Name
	}
	vms := make([]VM, 0, len(raw))
	for i := range raw {
		vm := VM{Name: raw[i].Name, PowerState: string(raw[i].Runtime.PowerState)}
		if ref := raw[i].Runtime.Host; ref != nil {
			vm.Host = hostNames[*ref]
		}
		vms = append(vms, vm)
	}
	return clusters, vms, nil
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
