package vsphere

import (
	"context"
	"sort"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

var hostProps = []string{
	"name",
	"parent",
	"runtime.powerState",
	"runtime.connectionState",
	"runtime.inMaintenanceMode",
	"summary.hardware",
	"summary.quickStats",
	"summary.config.product",
	"vm",
}

// ListHosts returns the ESXi hosts in a vCenter.
func (c *Client) ListHosts(ctx context.Context) ([]Host, error) {
	idx, err := newIndex(ctx, c)
	if err != nil {
		return nil, err
	}
	return c.listHosts(ctx, idx)
}

func (c *Client) listHosts(ctx context.Context, idx *index) ([]Host, error) {
	var raw []mo.HostSystem
	if err := retrieve(ctx, c, []string{"HostSystem"}, []string{"HostSystem"}, hostProps, &raw); err != nil {
		return nil, err
	}
	out := make([]Host, 0, len(raw))
	for i := range raw {
		out = append(out, newHost(c, idx, &raw[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func newHost(c *Client, idx *index, m *mo.HostSystem) Host {
	h := Host{
		Location:        idx.locate(c, m.Self, m.Name),
		ID:              m.Self.Value,
		Name:            m.Name,
		PowerState:      string(m.Runtime.PowerState),
		ConnectionState: string(m.Runtime.ConnectionState),
		VMCount:         len(m.Vm),
	}
	if m.Runtime.InMaintenanceMode {
		h.InMaintenance = true
	}
	if m.Parent != nil && m.Parent.Type == "ClusterComputeResource" {
		h.Cluster = idx.name(m.Parent)
	}
	if hw := m.Summary.Hardware; hw != nil {
		h.Vendor = hw.Vendor
		h.Model = hw.Model
		h.CPUCores = int32(hw.NumCpuCores)
		h.CPUThreads = int32(hw.NumCpuThreads)
		h.CPUMHz = hw.CpuMhz
		h.MemoryMB = hw.MemorySize / (1 << 20)
	}
	if p := m.Summary.Config.Product; p != nil {
		h.Version = p.Version
		h.Build = p.Build
	}
	qs := m.Summary.QuickStats
	if qs.OverallCpuUsage != 0 {
		h.CPUUsageMHz = int64(qs.OverallCpuUsage)
	}
	if qs.OverallMemoryUsage != 0 {
		h.MemoryUsageMB = int64(qs.OverallMemoryUsage)
	}
	return h
}

// hostRefsByCluster is used by the cluster listing to count member hosts when
// the server does not populate the compute resource summary.
func hostRefsByCluster(refs []types.ManagedObjectReference) int { return len(refs) }
