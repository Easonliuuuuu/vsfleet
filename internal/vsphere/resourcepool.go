package vsphere

import (
	"context"
	"sort"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

var resourcePoolProps = []string{"name", "parent", "owner", "vm", "config", "summary", "overallStatus", "configStatus"}

func (c *Client) listResourcePools(ctx context.Context, idx *index) ([]ResourcePool, error) {
	var raw []mo.ResourcePool
	if err := retrieve(ctx, c, idx.root, []string{"ResourcePool"}, []string{"ResourcePool"}, resourcePoolProps, &raw); err != nil {
		return nil, err
	}

	out := make([]ResourcePool, 0, len(raw))
	for i := range raw {
		m := &raw[i]
		// A ContainerView of ResourcePool also returns VirtualApp, which is a
		// subtype. vsfleet already models vApps as their own kind.
		if m.Self.Type != "ResourcePool" {
			continue
		}
		out = append(out, newResourcePool(c, idx, m))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func newResourcePool(c *Client, idx *index, m *mo.ResourcePool) ResourcePool {
	parent := m.Parent
	root := parent == nil || parent.Type != "ResourcePool"
	pool := ResourcePool{
		Location:        idx.locate(c, m.Self, m.Name),
		ID:              m.Self.Value,
		Name:            m.Name,
		Root:            root,
		Parent:          idx.name(parent),
		Owner:           idx.name(&m.Owner),
		Status:          string(m.OverallStatus),
		ConfigStatus:    string(m.ConfigStatus),
		CPUExpandable:   m.Config.CpuAllocation.ExpandableReservation != nil && *m.Config.CpuAllocation.ExpandableReservation,
		CPUShares:       sharesValue(m.Config.CpuAllocation.Shares),
		CPULevel:        string(sharesLevel(m.Config.CpuAllocation.Shares)),
		MemConfiguredMB: resourcePoolConfiguredMemory(m.Summary),
		MemExpandable:   m.Config.MemoryAllocation.ExpandableReservation != nil && *m.Config.MemoryAllocation.ExpandableReservation,
		MemShares:       sharesValue(m.Config.MemoryAllocation.Shares),
		MemLevel:        string(sharesLevel(m.Config.MemoryAllocation.Shares)),
	}
	pool.VMRefs = make([]string, 0, len(m.Vm))
	for _, ref := range m.Vm {
		if ref.Value != "" {
			pool.VMRefs = append(pool.VMRefs, ref.Value)
		}
	}
	sort.Strings(pool.VMRefs)
	pool.CPUReservationMHz = m.Config.CpuAllocation.Reservation
	pool.CPULimitMHz = m.Config.CpuAllocation.Limit
	pool.CPUOverheadLimitMHz = m.Config.CpuAllocation.OverheadLimit
	pool.MemReservationMB = m.Config.MemoryAllocation.Reservation
	pool.MemLimitMB = m.Config.MemoryAllocation.Limit
	pool.MemOverheadLimitMB = m.Config.MemoryAllocation.OverheadLimit
	return pool
}

func resourcePoolConfiguredMemory(summary types.BaseResourcePoolSummary) int64 {
	resourcePool, ok := summary.(*types.ResourcePoolSummary)
	if !ok || resourcePool == nil {
		return 0
	}
	return int64(resourcePool.ConfiguredMemoryMB)
}

func sharesValue(shares *types.SharesInfo) int32 {
	if shares == nil {
		return 0
	}
	return shares.Shares
}

func sharesLevel(shares *types.SharesInfo) types.SharesLevel {
	if shares == nil {
		return ""
	}
	return shares.Level
}
