package vsphere

import (
	"context"
	"sort"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

var computeResourceKinds = []string{"ComputeResource", "ClusterComputeResource"}

var clusterProps = []string{"name", "parent", "summary", "host"}

// ListClusters returns the clusters in a vCenter. A host that is not in a
// cluster appears as a standalone compute resource and is reported with
// Standalone set, because operators still need to see where it lives.
func (c *Client) ListClusters(ctx context.Context) ([]Cluster, error) {
	idx, err := newIndex(ctx, c)
	if err != nil {
		return nil, err
	}
	return c.listClusters(ctx, idx)
}

func (c *Client) listClusters(ctx context.Context, idx *index) ([]Cluster, error) {
	var raw []mo.ComputeResource
	if err := retrieve(ctx, c, computeResourceKinds, []string{"ComputeResource"}, clusterProps, &raw); err != nil {
		return nil, err
	}
	settings, err := c.clusterSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Cluster, 0, len(raw))
	for i := range raw {
		m := &raw[i]
		cl := Cluster{
			Location:   idx.locate(c, m.Self, m.Name),
			ID:         m.Self.Value,
			Name:       m.Name,
			Standalone: m.Self.Type != "ClusterComputeResource",
			// The compute resource summary is authoritative when the server
			// fills it in; the member list is the fallback.
			Hosts: len(m.Host),
		}
		if s, ok := m.Summary.(*types.ComputeResourceSummary); ok && s != nil {
			cl.CPUCores = int32(s.NumCpuCores)
			cl.TotalCPUMHz = int64(s.TotalCpu)
			cl.TotalMemoryMB = s.TotalMemory / (1 << 20)
			cl.EffectiveHost = int(s.NumEffectiveHosts)
			if s.NumHosts > 0 {
				cl.Hosts = int(s.NumHosts)
			}
		}
		if st, ok := settings[m.Self]; ok {
			cl.DRSEnabled = st.drs
			cl.HAEnabled = st.ha
		}
		out = append(out, cl)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type clusterSetting struct{ drs, ha bool }

// clusterSettings reads DRS and HA state, which only exists on the cluster
// subtype and so cannot be part of the ComputeResource property set.
func (c *Client) clusterSettings(ctx context.Context) (map[types.ManagedObjectReference]clusterSetting, error) {
	var raw []mo.ClusterComputeResource
	err := retrieve(ctx, c, []string{"ClusterComputeResource"}, []string{"ClusterComputeResource"}, []string{"configurationEx"}, &raw)
	if err != nil {
		return nil, err
	}
	out := make(map[types.ManagedObjectReference]clusterSetting, len(raw))
	for i := range raw {
		cfg, ok := raw[i].ConfigurationEx.(*types.ClusterConfigInfoEx)
		if !ok || cfg == nil {
			continue
		}
		var s clusterSetting
		if cfg.DrsConfig.Enabled != nil {
			s.drs = *cfg.DrsConfig.Enabled
		}
		if cfg.DasConfig.Enabled != nil {
			s.ha = *cfg.DasConfig.Enabled
		}
		out[raw[i].Self] = s
	}
	return out, nil
}
