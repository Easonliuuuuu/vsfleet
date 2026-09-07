package vsphere

import (
	"context"
	"sort"

	"github.com/vmware/govmomi/vim25/mo"
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

var hostConfigProps = []string{"config.storageDevice", "config.network"}

// ListHosts returns the ESXi hosts in a vCenter.
func (c *Client) ListHosts(ctx context.Context) ([]Host, error) {
	idx, err := newIndex(ctx, c)
	if err != nil {
		return nil, err
	}
	return c.listHosts(ctx, idx)
}

func (c *Client) listHosts(ctx context.Context, idx *index) ([]Host, error) {
	return c.listHostsWith(ctx, idx, false)
}

func (c *Client) listHostsWith(ctx context.Context, idx *index, withConfig bool) ([]Host, error) {
	var raw []mo.HostSystem
	props := hostProps
	if withConfig {
		props = append(append([]string(nil), props...), hostConfigProps...)
	}
	if err := retrieve(ctx, c, idx.root, []string{"HostSystem"}, []string{"HostSystem"}, props, &raw); err != nil {
		return nil, err
	}
	out := make([]Host, 0, len(raw))
	for i := range raw {
		out = append(out, newHostWithConfig(c, idx, &raw[i], withConfig))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func newHostWithConfig(c *Client, idx *index, m *mo.HostSystem, withConfig bool) Host {
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
		h.TotalCPUMHz = int64(hw.NumCpuCores) * int64(hw.CpuMhz)
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
	if withConfig {
		mapHostConfig(&h, m.Config)
	}
	return h
}
