package vsphere

import (
	"context"
	"sort"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

var vappProps = []string{
	"name",
	"parent",
	"parentVApp",
	"vm",
	"resourcePool",
	"childLink",
	"summary",
}

// ListVApps returns vSphere VirtualApp containers in the client's configured
// datacenter scope. It is deliberately separate from ListClusters: a vApp is
// a logical workload container, not a compute resource.
func (c *Client) ListVApps(ctx context.Context) ([]VApp, error) {
	idx, err := newIndex(ctx, c)
	if err != nil {
		return nil, err
	}
	return c.listVApps(ctx, idx)
}

func (c *Client) listVApps(ctx context.Context, idx *index) ([]VApp, error) {
	var raw []mo.VirtualApp
	if err := retrieve(ctx, c, idx.root, []string{"VirtualApp"}, []string{"VirtualApp"}, vappProps, &raw); err != nil {
		return nil, err
	}
	out := make([]VApp, 0, len(raw))
	for i := range raw {
		out = append(out, newVApp(c, idx, &raw[i]))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func newVApp(c *Client, idx *index, m *mo.VirtualApp) VApp {
	parent := m.Parent
	parentVApp := m.ParentVApp
	if parentVApp == nil && parent != nil && parent.Type == "VirtualApp" {
		parentVApp = parent
	}

	childRefs := append([]types.ManagedObjectReference(nil), m.ResourcePool.ResourcePool...)
	for _, link := range m.ChildLink {
		childRefs = append(childRefs, link.Key)
	}
	childRefs = uniqueRefs(childRefs, m.Self)

	var childVApps, childPools []types.ManagedObjectReference
	for _, ref := range childRefs {
		switch ref.Type {
		case "VirtualApp":
			childVApps = append(childVApps, ref)
		case "ResourcePool":
			childPools = append(childPools, ref)
		}
	}

	loc := idx.locate(c, m.Self, m.Name)
	placement := idx.computeResourceOf(parent)
	if placement == "" {
		placement = idx.computeResourceOf(&m.Self)
	}
	cluster := idx.clusterFor(parent)
	if cluster == "" {
		cluster = idx.clusterFor(&m.Self)
	}
	directVMRefs := uniqueRefs(m.Vm)

	return VApp{
		Location:               loc,
		ID:                     m.Self.Value,
		Name:                   m.Name,
		Status:                 virtualAppStatus(m.Summary),
		ParentContainer:        idx.name(parent),
		ParentVApp:             idx.name(parentVApp),
		DirectVMCount:          len(directVMRefs),
		DirectVMs:              idx.names(directVMRefs),
		DirectVMRefs:           managedRefNames(directVMRefs),
		ChildVAppCount:         len(childVApps),
		ChildVApps:             idx.names(childVApps),
		ChildVAppRefs:          managedRefNames(childVApps),
		ChildResourcePoolCount: len(childPools),
		ChildResourcePools:     idx.names(childPools),
		ChildResourcePoolRefs:  managedRefNames(childPools),
		Cluster:                cluster,
		ComputeResource:        placement,
	}
}

func virtualAppStatus(summary types.BaseResourcePoolSummary) string {
	vapp, ok := summary.(*types.VirtualAppSummary)
	if !ok || vapp == nil {
		return "unknown"
	}
	if vapp.VAppState != "" {
		return string(vapp.VAppState)
	}
	if vapp.Suspended != nil && *vapp.Suspended {
		return "suspended"
	}
	return "unknown"
}

func uniqueRefs(refs []types.ManagedObjectReference, exclude ...types.ManagedObjectReference) []types.ManagedObjectReference {
	seen := make(map[types.ManagedObjectReference]bool, len(refs)+len(exclude))
	for _, ref := range exclude {
		seen[ref] = true
	}
	out := make([]types.ManagedObjectReference, 0, len(refs))
	for _, ref := range refs {
		if ref.Type == "" || ref.Value == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out
}

func managedRefNames(refs []types.ManagedObjectReference) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.Type+":"+ref.Value)
	}
	sort.Strings(out)
	return out
}
