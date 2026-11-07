package vsphere

import (
	"context"
	"fmt"
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
	// ID is the datastore cluster's managed object ID, empty in a capture
	// predating inventory schema 38.
	ID            string `json:"id,omitempty"`
	Name          string `json:"name"`
	CapacityBytes *int64 `json:"capacity_bytes,omitempty"`
	FreeBytes     *int64 `json:"free_bytes,omitempty"`
	// StorageDRS is the cluster's Storage DRS space configuration; nil when
	// it was not read.
	StorageDRS *StorageDRS `json:"storage_drs,omitempty"`
}

// StorageDRS is a datastore cluster's Storage DRS space configuration, as
// vCenter reports it. vCenter leaves a value it defaults unset: an empty
// mode is "utilization", and zero thresholds are its 80% and 50 GB.
type StorageDRS struct {
	Enabled bool `json:"enabled"`
	// SpaceThresholdMode is "utilization" or "freeSpace": whether Storage
	// DRS avoids a datastore above SpaceUtilizationPct used, or one below
	// FreeSpaceGB free.
	SpaceThresholdMode  string `json:"space_threshold_mode,omitempty"`
	SpaceUtilizationPct int32  `json:"space_utilization_threshold_pct,omitempty"`
	FreeSpaceGB         int32  `json:"free_space_threshold_gb,omitempty"`
}

// Storage DRS defaults for a value vCenter leaves unset.
const (
	StorageDRSDefaultUtilizationPct = 80
	StorageDRSDefaultFreeSpaceGB    = 50
)

// AboveThreshold reports whether Storage DRS avoids a datastore of this
// capacity and free space during initial placement, and the threshold it
// applied, e.g. "80% used" or "50 GB free".
func (s StorageDRS) AboveThreshold(capacity, free int64) (bool, string) {
	if s.SpaceThresholdMode == string(types.StorageDrsSpaceLoadBalanceConfigSpaceThresholdModeFreeSpace) {
		gb := s.FreeSpaceGB
		if gb <= 0 {
			gb = StorageDRSDefaultFreeSpaceGB
		}
		return free < int64(gb)<<30, fmt.Sprintf("%d GB free", gb)
	}
	pct := s.SpaceUtilizationPct
	if pct <= 0 {
		pct = StorageDRSDefaultUtilizationPct
	}
	if capacity <= 0 {
		return false, fmt.Sprintf("%d%% used", pct)
	}
	return float64(capacity-free)/float64(capacity)*100 > float64(pct), fmt.Sprintf("%d%% used", pct)
}

// datastoreDetails reads m's details. pods holds each datastore cluster's
// summary and Storage DRS configuration, by reference.
func datastoreDetails(idx *index, m *mo.Datastore, pods map[types.ManagedObjectReference]*mo.StoragePod) *DatastoreDetails {
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
		pod := &DatastoreCluster{ID: m.Parent.Value, Name: idx.name(m.Parent)}
		if p := pods[*m.Parent]; p != nil {
			if s := p.Summary; s != nil {
				capacity, free := s.Capacity, s.FreeSpace
				pod.CapacityBytes, pod.FreeBytes = &capacity, &free
				if pod.Name == "" {
					pod.Name = s.Name
				}
			}
			pod.StorageDRS = storageDRS(p)
		}
		d.StorageCluster = pod
	}
	return d
}

// storagePodSummaries reads every datastore cluster's summary. It is best
// effort: a failure leaves the clusters' sizes unread, not the datastores.
func (c *Client) storagePodSummaries(ctx context.Context, idx *index) map[types.ManagedObjectReference]*mo.StoragePod {
	var raw []mo.StoragePod
	if err := retrieve(ctx, c, idx.root, []string{"StoragePod"}, []string{"StoragePod"}, storagePodProps, &raw); err != nil {
		return nil
	}
	out := make(map[types.ManagedObjectReference]*mo.StoragePod, len(raw))
	for i := range raw {
		out[raw[i].Self] = &raw[i]
	}
	return out
}

// storagePodProps are read for each datastore cluster: its size and its
// Storage DRS space configuration.
var storagePodProps = []string{
	"summary",
	"podStorageDrsEntry.storageDrsConfig.podConfig.enabled",
	"podStorageDrsEntry.storageDrsConfig.podConfig.spaceLoadBalanceConfig",
}

// storageDRS is a datastore cluster's Storage DRS space configuration, nil
// when it was not read.
func storageDRS(pod *mo.StoragePod) *StorageDRS {
	if pod == nil || pod.PodStorageDrsEntry == nil {
		return nil
	}
	cfg := pod.PodStorageDrsEntry.StorageDrsConfig.PodConfig
	out := &StorageDRS{Enabled: cfg.Enabled}
	if space := cfg.SpaceLoadBalanceConfig; space != nil {
		out.SpaceThresholdMode = space.SpaceThresholdMode
		out.SpaceUtilizationPct = space.SpaceUtilizationThreshold
		out.FreeSpaceGB = space.FreeSpaceThresholdGB
	}
	return out
}
