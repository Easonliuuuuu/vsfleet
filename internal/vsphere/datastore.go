package vsphere

import (
	"context"
	"sort"

	"github.com/vmware/govmomi/vim25/mo"
)

var datastoreProps = []string{"name", "parent", "summary"}

// ListDatastores returns the datastores in a vCenter.
func (c *Client) ListDatastores(ctx context.Context) ([]Datastore, error) {
	idx, err := newIndex(ctx, c)
	if err != nil {
		return nil, err
	}
	return c.listDatastores(ctx, idx)
}

func (c *Client) listDatastores(ctx context.Context, idx *index) ([]Datastore, error) {
	var raw []mo.Datastore
	if err := retrieve(ctx, c, idx.root, []string{"Datastore"}, []string{"Datastore"}, datastoreProps, &raw); err != nil {
		return nil, err
	}
	out := make([]Datastore, 0, len(raw))
	for i := range raw {
		m := &raw[i]
		s := m.Summary
		out = append(out, Datastore{
			Location:      idx.locate(c, m.Self, m.Name),
			ID:            m.Self.Value,
			Name:          m.Name,
			Type:          s.Type,
			Accessible:    s.Accessible,
			CapacityBytes: s.Capacity,
			FreeBytes:     s.FreeSpace,
			Maintenance:   s.MaintenanceMode,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
