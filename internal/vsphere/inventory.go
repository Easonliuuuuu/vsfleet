package vsphere

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// entityKinds are the managed object types walked to build the inventory path
// index. Everything a domain object can live inside has to appear here.
var entityKinds = []string{
	"Folder",
	"Datacenter",
	"ComputeResource",
	"ClusterComputeResource",
	"ResourcePool",
	"VirtualApp",
	"HostSystem",
	"VirtualMachine",
	"Datastore",
	"StoragePod",
	"Network",
	"DistributedVirtualPortgroup",
	"DistributedVirtualSwitch",
}

type entity struct {
	ref    types.ManagedObjectReference
	name   string
	parent *types.ManagedObjectReference
}

// index maps managed object references to their name and parent so inventory
// paths, datacenters and clusters can be resolved locally instead of with one
// round trip per object.
type index struct {
	byRef map[types.ManagedObjectReference]entity
}

func newIndex(ctx context.Context, c *Client) (*index, error) {
	var ents []mo.ManagedEntity
	err := retrieve(ctx, c, entityKinds, []string{"ManagedEntity"}, []string{"name", "parent"}, &ents)
	if err != nil {
		return nil, fmt.Errorf("build inventory index: %w", err)
	}
	idx := &index{byRef: make(map[types.ManagedObjectReference]entity, len(ents))}
	for _, e := range ents {
		idx.byRef[e.Self] = entity{ref: e.Self, name: e.Name, parent: e.Parent}
	}
	return idx, nil
}

// ancestors returns the chain from the object up to, but not including, the
// root folder. The object itself is first.
func (i *index) ancestors(ref types.ManagedObjectReference) []entity {
	var chain []entity
	seen := make(map[types.ManagedObjectReference]bool)
	cur := &ref
	for cur != nil && !seen[*cur] {
		seen[*cur] = true
		e, ok := i.byRef[*cur]
		if !ok {
			break
		}
		chain = append(chain, e)
		cur = e.parent
	}
	return chain
}

// path renders the full inventory path, e.g. /Taipei/vm/Templates/ubuntu-24.
// leaf overrides the name of the object itself, which matters for the handful
// of types that report a display name only through their own summary.
func (i *index) path(ref types.ManagedObjectReference, leaf string) string {
	chain := i.ancestors(ref)
	if len(chain) == 0 {
		if leaf == "" {
			return ""
		}
		return "/" + leaf
	}
	if leaf != "" {
		chain[0].name = leaf
	}
	parts := make([]string, 0, len(chain))
	for n := len(chain) - 1; n >= 0; n-- {
		parts = append(parts, chain[n].name)
	}
	return "/" + strings.Join(parts, "/")
}

// datacenter returns the name of the datacenter an object belongs to.
func (i *index) datacenter(ref types.ManagedObjectReference) string {
	for _, e := range i.ancestors(ref) {
		if e.ref.Type == "Datacenter" {
			return e.name
		}
	}
	return ""
}

// name returns the display name of a reference, or "" when unknown.
func (i *index) name(ref *types.ManagedObjectReference) string {
	if ref == nil {
		return ""
	}
	return i.byRef[*ref].name
}

// names resolves a list of references, dropping any that are unknown.
func (i *index) names(refs []types.ManagedObjectReference) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if n := i.name(&r); n != "" {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// clusterOf returns the cluster a host belongs to, empty for a standalone host.
func (i *index) clusterOf(ref *types.ManagedObjectReference) string {
	if ref == nil {
		return ""
	}
	e, ok := i.byRef[*ref]
	if !ok {
		return ""
	}
	if e.parent != nil && e.parent.Type == "ClusterComputeResource" {
		return i.name(e.parent)
	}
	return ""
}

// folderPath returns a VM's folder relative to its datacenter, so that
// /Taipei/vm/Templates/Linux/ubuntu reads as /Templates/Linux.
func (i *index) folderPath(parent *types.ManagedObjectReference, datacenter string) string {
	if parent == nil {
		return "/"
	}
	full := i.path(*parent, "")
	if full == "" {
		return "/"
	}
	for _, root := range []string{"/" + datacenter + "/vm", "/" + datacenter + "/host", "/" + datacenter + "/datastore", "/" + datacenter + "/network"} {
		if full == root {
			return "/"
		}
		if strings.HasPrefix(full, root+"/") {
			return strings.TrimPrefix(full, root)
		}
	}
	return full
}

func (i *index) locate(c *Client, ref types.ManagedObjectReference, name string) Location {
	return Location{
		Context:    c.Context.Name,
		Datacenter: i.datacenter(ref),
		Path:       i.path(ref, name),
	}
}

// retrieve enumerates every object of the given view kinds and loads the named
// properties into dst. propKinds are the types the property specification is
// written against, which is usually the common base type of the view kinds.
func retrieve(ctx context.Context, c *Client, viewKinds, propKinds, props []string, dst any) error {
	m := view.NewManager(c.VIM())
	v, err := m.CreateContainerView(ctx, c.VIM().ServiceContent.RootFolder, viewKinds, true)
	if err != nil {
		return fmt.Errorf("create container view: %w", err)
	}
	defer func() {
		// Destroying the view uses a background context so that a cancelled
		// request still releases the server-side object.
		_ = v.Destroy(context.WithoutCancel(ctx))
	}()
	if err := v.Retrieve(ctx, propKinds, props, dst); err != nil {
		return fmt.Errorf("retrieve %s properties: %w", strings.Join(propKinds, ","), err)
	}
	return nil
}

// ListInventory enumerates everything in one vCenter. The individual List
// functions exist for callers that need only one kind; this one is what the
// cache and the cross-context search use.
//
// It only fails outright when the path index itself cannot be built — every
// object's inventory path depends on it, so nothing else is usable either.
// A kind that fails to list on its own (a limited-permission account missing
// one privilege, say) is recorded in Inventory.Errors and does not stop the
// rest from being enumerated.
func (c *Client) ListInventory(ctx context.Context) (*Inventory, error) {
	reportStage(ctx, StageLoadingIndex)
	idx, err := newIndex(ctx, c)
	if err != nil {
		return nil, err
	}
	inv := &Inventory{Context: c.Context.Name}
	fail := func(kind Kind, err error) {
		inv.Errors = append(inv.Errors, InventoryError{Kind: kind, Message: err.Error()})
	}

	reportStage(ctx, StageLoadingVMs)
	if vms, err := c.listVMs(ctx, idx); err != nil {
		fail(KindVM, err)
		fail(KindTemplate, err)
	} else {
		for _, vm := range vms {
			if vm.IsTemplate {
				inv.Templates = append(inv.Templates, vm)
			} else {
				inv.VMs = append(inv.VMs, vm)
			}
		}
	}
	reportStage(ctx, StageLoadingHosts)
	if hosts, err := c.listHosts(ctx, idx); err != nil {
		fail(KindHost, err)
	} else {
		inv.Hosts = hosts
	}
	reportStage(ctx, StageLoadingClusters)
	if clusters, err := c.listClusters(ctx, idx); err != nil {
		fail(KindCluster, err)
	} else {
		inv.Clusters = clusters
	}
	reportStage(ctx, StageLoadingDatastores)
	if datastores, err := c.listDatastores(ctx, idx); err != nil {
		fail(KindDatastore, err)
	} else {
		inv.Datastores = datastores
	}
	reportStage(ctx, StageLoadingNetworks)
	if networks, err := c.listNetworks(ctx, idx); err != nil {
		fail(KindNetwork, err)
	} else {
		inv.Networks = networks
	}
	return inv, nil
}
