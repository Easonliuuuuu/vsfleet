package vsphere

import (
	"context"
	"sort"
	"strings"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// datastoreDetailProps are read with the datastore list for its details.
var datastoreDetailProps = []string{"configStatus", "host", "vm", "iormConfiguration"}

// DatastoreDetails is the datastore configuration an RVTools vDatastore
// sheet reports beyond capacity. Nil means not read: a capture predating
// inventory schema 35.
type DatastoreDetails struct {
	// ConfigStatus is vSphere's configuration status colour.
	ConfigStatus string `json:"config_status,omitempty"`
	// Address is where the storage lives: a VMFS datastore's extent device
	// names, an NFS datastore's server and export, e.g. "nas01 /srv/vm".
	Address string `json:"address,omitempty"`
	// Hosts names the hosts that mount the datastore, sorted.
	Hosts []string `json:"hosts,omitempty"`
	// VMIDs are the VMs and templates with files on the datastore, sorted.
	// A capture records an empty list for none; nil means not read, as in an
	// RVTools import, which carries only the counts.
	VMIDs []string `json:"vm_ids"`
	// MultipleHostAccess reports that more than one host can access it.
	MultipleHostAccess *bool `json:"multiple_host_access,omitempty"`
	// SIOCEnabled and SIOCThreshold are Storage I/O Control and its
	// congestion threshold in milliseconds; nil when vSphere reports none.
	SIOCEnabled   *bool  `json:"sioc_enabled,omitempty"`
	SIOCThreshold *int32 `json:"sioc_threshold,omitempty"`
	// VMFS is the file system of a VMFS datastore, nil for any other type.
	VMFS *DatastoreVMFS `json:"vmfs,omitempty"`
	// StorageCluster is the datastore cluster (storage pod) it belongs to,
	// nil when it belongs to none.
	StorageCluster *DatastoreCluster `json:"storage_cluster,omitempty"`
}

// DatastoreVMFS is a VMFS volume's file system.
type DatastoreVMFS struct {
	BlockSizeMB  int32  `json:"block_size_mb"`
	MaxBlocks    int32  `json:"max_blocks"`
	Extents      int    `json:"extents"`
	MajorVersion int32  `json:"major_version"`
	Version      string `json:"version,omitempty"`
	Upgradable   bool   `json:"upgradable"`
}

// DatastoreCluster is a datastore cluster's name and its capacity across
// its member datastores; the sizes are nil when the cluster's summary could
// not be read.
type DatastoreCluster struct {
	Name          string `json:"name"`
	CapacityBytes *int64 `json:"capacity_bytes,omitempty"`
	FreeBytes     *int64 `json:"free_bytes,omitempty"`
}

// datastoreDetails reads m's details. pods holds each datastore cluster's
// summary, by reference.
func datastoreDetails(idx *index, m *mo.Datastore, pods map[types.ManagedObjectReference]*types.StoragePodSummary) *DatastoreDetails {
	d := &DatastoreDetails{ConfigStatus: string(m.ConfigStatus)}
	access := m.Summary.MultipleHostAccess
	d.MultipleHostAccess = clonePtr(access)
	for _, mount := range m.Host {
		if name := idx.name(&mount.Key); name != "" {
			d.Hosts = append(d.Hosts, name)
		} else {
			d.Hosts = append(d.Hosts, mount.Key.Value)
		}
	}
	sort.Strings(d.Hosts)
	d.VMIDs = make([]string, 0, len(m.Vm))
	for _, vm := range m.Vm {
		d.VMIDs = append(d.VMIDs, vm.Value)
	}
	sort.Strings(d.VMIDs)
	if c := m.IormConfiguration; c != nil {
		d.SIOCEnabled = &c.Enabled
		threshold := c.CongestionThreshold
		d.SIOCThreshold = &threshold
	}
	switch info := m.Info.(type) {
	case *types.VmfsDatastoreInfo:
		if info != nil && info.Vmfs != nil {
			v := info.Vmfs
			var disks []string
			for _, e := range v.Extent {
				disks = append(disks, e.DiskName)
			}
			d.Address = strings.Join(disks, ", ")
			d.VMFS = &DatastoreVMFS{BlockSizeMB: v.BlockSizeMb, MaxBlocks: v.MaxBlocks, Extents: len(v.Extent), MajorVersion: v.MajorVersion, Version: v.Version, Upgradable: v.VmfsUpgradable}
		}
	case *types.NasDatastoreInfo:
		if info != nil && info.Nas != nil {
			d.Address = strings.TrimSpace(info.Nas.RemoteHost + " " + info.Nas.RemotePath)
		}
	}
	if m.Parent != nil && m.Parent.Type == "StoragePod" {
		pod := &DatastoreCluster{Name: idx.name(m.Parent)}
		if s := pods[*m.Parent]; s != nil {
			capacity, free := s.Capacity, s.FreeSpace
			pod.CapacityBytes, pod.FreeBytes = &capacity, &free
			if pod.Name == "" {
				pod.Name = s.Name
			}
		}
		d.StorageCluster = pod
	}
	return d
}

// storagePodSummaries reads every datastore cluster's summary. It is best
// effort: a failure leaves the clusters' sizes unread, not the datastores.
func (c *Client) storagePodSummaries(ctx context.Context, idx *index) map[types.ManagedObjectReference]*types.StoragePodSummary {
	var raw []mo.StoragePod
	if err := retrieve(ctx, c, idx.root, []string{"StoragePod"}, []string{"StoragePod"}, []string{"summary"}, &raw); err != nil {
		return nil
	}
	out := make(map[types.ManagedObjectReference]*types.StoragePodSummary, len(raw))
	for i := range raw {
		out[raw[i].Self] = raw[i].Summary
	}
	return out
}
