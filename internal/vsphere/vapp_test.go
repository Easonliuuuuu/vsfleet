package vsphere

import (
	"testing"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/easonliuuuuu/vsfleet/internal/config"
)

func TestNewVAppKeepsDirectAndNestedMembershipDistinct(t *testing.T) {
	dc := types.ManagedObjectReference{Type: "Datacenter", Value: "dc-1"}
	cr := types.ManagedObjectReference{Type: "ClusterComputeResource", Value: "domain-c1"}
	root := types.ManagedObjectReference{Type: "ResourcePool", Value: "resgroup-1"}
	app := types.ManagedObjectReference{Type: "VirtualApp", Value: "vapp-outer"}
	nested := types.ManagedObjectReference{Type: "VirtualApp", Value: "vapp-nested"}
	pool := types.ManagedObjectReference{Type: "ResourcePool", Value: "resgroup-child"}
	vm := types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-1"}

	idx := &index{byRef: map[types.ManagedObjectReference]entity{
		dc:     {ref: dc, name: "DC0"},
		cr:     {ref: cr, name: "compute-a", parent: &dc},
		root:   {ref: root, name: "Resources", parent: &cr},
		app:    {ref: app, name: "web-stack", parent: &root},
		nested: {ref: nested, name: "web-cache", parent: &app},
		pool:   {ref: pool, name: "web-pool", parent: &app},
		vm:     {ref: vm, name: "api-01", parent: &app},
	}}
	c := &Client{Context: &config.Context{Name: "prod"}}
	raw := &mo.VirtualApp{}
	raw.Self = app
	raw.Name = "web-stack"
	raw.Parent = &root
	raw.Vm = []types.ManagedObjectReference{vm, vm}
	raw.ResourcePool.ResourcePool = []types.ManagedObjectReference{nested, pool, nested}
	raw.ChildLink = []types.VirtualAppLinkInfo{{Key: nested}, {Key: pool}}
	raw.Summary = &types.VirtualAppSummary{VAppState: types.VirtualAppVAppStateStarted}

	got := newVApp(c, idx, raw)
	if got.Status != "started" || got.Datacenter != "DC0" || got.Cluster != "compute-a" || got.ComputeResource != "compute-a" {
		t.Fatalf("vApp identity/status/placement = %+v", got)
	}
	if got.DirectVMCount != 1 || len(got.DirectVMs) != 1 || got.DirectVMs[0] != "api-01" {
		t.Fatalf("direct VM membership = %+v", got)
	}
	if got.ChildVAppCount != 1 || len(got.ChildVApps) != 1 || got.ChildVApps[0] != "web-cache" {
		t.Fatalf("nested vApp membership = %+v", got)
	}
	if len(got.ChildVAppRefs) != 1 || got.ChildVAppRefs[0] != "VirtualApp:vapp-nested" {
		t.Fatalf("nested vApp references = %+v", got.ChildVAppRefs)
	}
	if got.ChildResourcePoolCount != 1 || len(got.ChildResourcePools) != 1 || got.ChildResourcePools[0] != "web-pool" {
		t.Fatalf("resource-pool membership = %+v", got)
	}
	if len(got.ChildResourcePoolRefs) != 1 || got.ChildResourcePoolRefs[0] != "ResourcePool:resgroup-child" {
		t.Fatalf("resource-pool references = %+v", got.ChildResourcePoolRefs)
	}
}
