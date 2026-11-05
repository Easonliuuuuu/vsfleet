package vsphere

import (
	"reflect"
	"testing"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// The datastores are the nested lab's local-esxi01 (VMFS 6) and nfs-shared
// (NFS 3) as vCenter 8.0.3 reports them.
func TestDatastoreDetailsReadsTheLabsDatastores(t *testing.T) {
	esxi01 := types.ManagedObjectReference{Type: "HostSystem", Value: "host-12"}
	esxi02 := types.ManagedObjectReference{Type: "HostSystem", Value: "host-17"}
	pod := types.ManagedObjectReference{Type: "StoragePod", Value: "group-p1"}
	idx := &index{byRef: map[types.ManagedObjectReference]entity{
		esxi01: {ref: esxi01, name: "192.168.150.11"}, esxi02: {ref: esxi02, name: "192.168.150.12"}, pod: {ref: pod, name: "pod-a"},
	}}
	yes, no := true, false
	threshold := int32(30)
	vms := func(ids ...string) []types.ManagedObjectReference {
		var out []types.ManagedObjectReference
		for _, id := range ids {
			out = append(out, types.ManagedObjectReference{Type: "VirtualMachine", Value: id})
		}
		return out
	}
	iorm := &types.StorageIORMInfo{Enabled: false, CongestionThreshold: 30}
	for _, tc := range []struct {
		name string
		ds   mo.Datastore
		pods map[types.ManagedObjectReference]*types.StoragePodSummary
		want *DatastoreDetails
	}{
		{"local-esxi01", mo.Datastore{
			ManagedEntity: mo.ManagedEntity{ConfigStatus: "gray"},
			Summary:       types.DatastoreSummary{MultipleHostAccess: &no},
			Host:          []types.DatastoreHostMount{{Key: esxi01}},
			Vm:            vms("vm-1016", "vm-1008"), IormConfiguration: iorm,
			Info: &types.VmfsDatastoreInfo{Vmfs: &types.HostVmfsVolume{BlockSizeMb: 1, MaxBlocks: 63963136, MajorVersion: 6, Version: "6.82",
				Extent: []types.HostScsiDiskPartition{{DiskName: "t10.ATA_____QEMU_HARDDISK___________________________esxi01local_________", Partition: 1}}}},
		}, nil, &DatastoreDetails{ConfigStatus: "gray", Address: "t10.ATA_____QEMU_HARDDISK___________________________esxi01local_________",
			Hosts: []string{"192.168.150.11"}, VMIDs: []string{"vm-1008", "vm-1016"}, MultipleHostAccess: &no, SIOCEnabled: &no, SIOCThreshold: &threshold,
			VMFS: &DatastoreVMFS{BlockSizeMB: 1, MaxBlocks: 63963136, Extents: 1, MajorVersion: 6, Version: "6.82"}}},
		{"nfs-shared", mo.Datastore{
			ManagedEntity:     mo.ManagedEntity{ConfigStatus: "gray"},
			Summary:           types.DatastoreSummary{MultipleHostAccess: &yes},
			Host:              []types.DatastoreHostMount{{Key: esxi02}, {Key: esxi01}},
			IormConfiguration: iorm,
			Info:              &types.NasDatastoreInfo{Nas: &types.HostNasVolume{RemoteHost: "192.168.150.1", RemotePath: "/srv/vsphere"}},
		}, nil, &DatastoreDetails{ConfigStatus: "gray", Address: "192.168.150.1 /srv/vsphere", Hosts: []string{"192.168.150.11", "192.168.150.12"},
			VMIDs: []string{}, MultipleHostAccess: &yes, SIOCEnabled: &no, SIOCThreshold: &threshold}},
		{"in a datastore cluster", mo.Datastore{ManagedEntity: mo.ManagedEntity{Parent: &pod}},
			map[types.ManagedObjectReference]*types.StoragePodSummary{pod: {Name: "pod-a", Capacity: 2 << 30, FreeSpace: 1 << 30}},
			&DatastoreDetails{VMIDs: []string{}, StorageCluster: &DatastoreCluster{Name: "pod-a", CapacityBytes: ptr(int64(2 << 30)), FreeBytes: ptr(int64(1 << 30))}}},
		{"in a datastore cluster whose summary was not read", mo.Datastore{ManagedEntity: mo.ManagedEntity{Parent: &pod}}, nil,
			&DatastoreDetails{VMIDs: []string{}, StorageCluster: &DatastoreCluster{Name: "pod-a"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := datastoreDetails(idx, &tc.ds, tc.pods); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("details = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
