package vsphere

import "context"

// FetchGroup identifies one concurrent unit of inventory retrieval. VMs and
// templates share a single vSphere call and so are always fetched together
// as one group; every other kind is its own.
type FetchGroup string

// Fetch groups, in the order a sequential caller retrieves them.
const (
	GroupVMs        FetchGroup = "vms"
	GroupHosts      FetchGroup = "hosts"
	GroupClusters   FetchGroup = "clusters"
	GroupVApps      FetchGroup = "vapps"
	GroupDatastores FetchGroup = "datastores"
	GroupNetworks   FetchGroup = "networks"
)

// AllGroups lists every fetch group ListInventory enumerates.
var AllGroups = []FetchGroup{GroupVMs, GroupHosts, GroupClusters, GroupVApps, GroupDatastores, GroupNetworks}

// GroupFor returns the fetch group that populates kind. VMs and templates
// map onto the same group, since starting on either tab prioritizes the one
// vSphere call that answers both.
func GroupFor(k Kind) FetchGroup {
	switch k {
	case KindVM, KindTemplate:
		return GroupVMs
	case KindHost:
		return GroupHosts
	case KindCluster:
		return GroupClusters
	case KindVApp:
		return GroupVApps
	case KindDatastore:
		return GroupDatastores
	case KindNetwork:
		return GroupNetworks
	default:
		return ""
	}
}

// Index is the shared inventory path index built once per operation — see
// Client.NewIndex — and reused by every FetchGroup call that follows, so a
// caller retrieving several groups for one context pays for the index build
// once rather than once per group.
type Index struct{ idx *index }

// NewIndex builds the shared path index for c's context, scoped to its
// configured datacenter (see resolveRoot). ListInventory builds one and
// walks every group sequentially; a caller wanting groups individually —
// prioritized, or fetched concurrently — builds one and calls FetchGroup
// against it directly instead, reusing the same Index for each.
func (c *Client) NewIndex(ctx context.Context) (*Index, error) {
	reportStage(ctx, StageLoadingIndex)
	idx, err := newIndex(ctx, c)
	if err != nil {
		return nil, err
	}
	return &Index{idx: idx}, nil
}

// FetchGroup retrieves one group's objects using idx, filling in only the
// Inventory fields that group owns. A failure to list is recorded in the
// returned Inventory.Errors rather than as a Go error — the same convention
// ListInventory uses for a kind a limited-permission account cannot see —
// so a caller applying several groups' results (see Inventory.ApplyGroup)
// never has to special-case a group that failed outright versus one that
// simply had nothing to report.
func (c *Client) FetchGroup(ctx context.Context, idx *Index, group FetchGroup) *Inventory {
	inv := &Inventory{Context: c.Context.Name}
	fail := func(err error, kinds ...Kind) {
		for _, k := range kinds {
			inv.Errors = append(inv.Errors, InventoryError{Kind: k, Message: err.Error()})
		}
	}
	switch group {
	case GroupVMs:
		reportStage(ctx, StageLoadingVMs)
		if vms, err := c.listVMs(ctx, idx.idx); err != nil {
			fail(err, KindVM, KindTemplate)
		} else {
			for _, vm := range vms {
				if vm.IsTemplate {
					inv.Templates = append(inv.Templates, vm)
				} else {
					inv.VMs = append(inv.VMs, vm)
				}
			}
		}
	case GroupHosts:
		reportStage(ctx, StageLoadingHosts)
		if hosts, err := c.listHosts(ctx, idx.idx); err != nil {
			fail(err, KindHost)
		} else {
			inv.Hosts = hosts
		}
	case GroupClusters:
		reportStage(ctx, StageLoadingClusters)
		if clusters, err := c.listClusters(ctx, idx.idx); err != nil {
			fail(err, KindCluster)
		} else {
			inv.Clusters = clusters
		}
	case GroupVApps:
		reportStage(ctx, StageLoadingVApps)
		if vapps, err := c.listVApps(ctx, idx.idx); err != nil {
			fail(err, KindVApp)
		} else {
			inv.VApps = vapps
		}
	case GroupDatastores:
		reportStage(ctx, StageLoadingDatastores)
		if datastores, err := c.listDatastores(ctx, idx.idx); err != nil {
			fail(err, KindDatastore)
		} else {
			inv.Datastores = datastores
		}
	case GroupNetworks:
		reportStage(ctx, StageLoadingNetworks)
		if networks, err := c.listNetworks(ctx, idx.idx); err != nil {
			fail(err, KindNetwork)
		} else {
			inv.Networks = networks
		}
	}
	return inv
}
