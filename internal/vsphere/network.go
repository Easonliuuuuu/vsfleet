package vsphere

import (
	"context"
	"sort"

	"github.com/vmware/govmomi/vim25/mo"
)

var networkKinds = []string{"Network", "DistributedVirtualPortgroup", "OpaqueNetwork"}

var networkProps = []string{"name", "parent", "summary"}

// ListNetworks returns the networks and port groups in a vCenter.
func (c *Client) ListNetworks(ctx context.Context) ([]Network, error) {
	idx, err := newIndex(ctx, c)
	if err != nil {
		return nil, err
	}
	return c.listNetworks(ctx, idx)
}

func (c *Client) listNetworks(ctx context.Context, idx *index) ([]Network, error) {
	var raw []mo.Network
	if err := retrieve(ctx, c, networkKinds, []string{"Network"}, networkProps, &raw); err != nil {
		return nil, err
	}
	out := make([]Network, 0, len(raw))
	for i := range raw {
		m := &raw[i]
		n := Network{
			ID:         m.Self.Value,
			Name:       m.Name,
			Type:       networkTypeName(m.Self.Type),
			Accessible: true,
		}
		if s := m.Summary; s != nil {
			if b := s.GetNetworkSummary(); b != nil {
				n.Accessible = b.Accessible
				if b.Name != "" {
					n.Name = b.Name
				}
			}
		}
		n.Location = idx.locate(c, m.Self, n.Name)
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func networkTypeName(kind string) string {
	switch kind {
	case "DistributedVirtualPortgroup":
		return "portgroup"
	case "OpaqueNetwork":
		return "opaque"
	default:
		return "standard"
	}
}
