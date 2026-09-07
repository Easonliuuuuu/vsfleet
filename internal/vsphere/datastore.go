package vsphere

import (
	"context"
	"sort"
	"strings"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

var datastoreProps = []string{"name", "parent", "summary", "browser", "info"}

// ListDatastores returns the datastores in a vCenter.
func (c *Client) ListDatastores(ctx context.Context) ([]Datastore, error) {
	idx, err := newIndex(ctx, c)
	if err != nil {
		return nil, err
	}
	return c.listDatastores(ctx, idx)
}

func (c *Client) listDatastores(ctx context.Context, idx *index) ([]Datastore, error) {
	return c.listDatastoresWith(ctx, idx, false)
}

func (c *Client) listDatastoresWith(ctx context.Context, idx *index, browse bool) ([]Datastore, error) {
	var raw []mo.Datastore
	if err := retrieve(ctx, c, idx.root, []string{"Datastore"}, []string{"Datastore"}, datastoreProps, &raw); err != nil {
		return nil, err
	}
	out := make([]Datastore, 0, len(raw))
	for i := range raw {
		m := &raw[i]
		s := m.Summary
		datastore := Datastore{
			Location:      idx.locate(c, m.Self, m.Name),
			ID:            m.Self.Value,
			Name:          m.Name,
			Type:          s.Type,
			Accessible:    s.Accessible,
			CapacityBytes: s.Capacity,
			FreeBytes:     s.FreeSpace,
			Maintenance:   s.MaintenanceMode,
			Backing:       datastoreBacking(m),
		}
		if browse {
			if !datastore.Accessible {
				datastore.BrowseStatus = "denied"
				datastore.BrowseError = "datastore is inaccessible"
			} else {
				datastore.Files, datastore.BrowseStatus, datastore.BrowseError, datastore.BrowseTruncated = c.browseDatastoreFiles(ctx, m.Name, m.Browser)
			}
		}
		out = append(out, datastore)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func datastoreBacking(m *mo.Datastore) DatastoreBacking {
	backing := DatastoreBacking{URL: strings.TrimSpace(m.Summary.Url)}
	switch info := m.Info.(type) {
	case *types.VmfsDatastoreInfo:
		if info != nil && info.Vmfs != nil {
			backing.VMFSUUID = strings.TrimSpace(info.Vmfs.Uuid)
			for _, extent := range info.Vmfs.Extent {
				if value := strings.TrimSpace(extent.DiskName); value != "" {
					backing.Extents = append(backing.Extents, value)
				}
			}
		}
	case *types.NasDatastoreInfo:
		if info != nil && info.Nas != nil {
			backing.NASRemote = strings.TrimSpace(info.Nas.RemoteHost + ":" + info.Nas.RemotePath)
		}
	case *types.VvolDatastoreInfo:
		if info != nil && info.VvolDS != nil {
			backing.VVolID = strings.TrimSpace(info.VvolDS.ScId)
		}
	case *types.LocalDatastoreInfo:
		backing.Local = info != nil
	}
	backing.Extents = dedupeSorted(backing.Extents)
	return backing
}

func dedupeSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	out := values[:0]
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}
