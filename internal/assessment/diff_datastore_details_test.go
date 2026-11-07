package assessment

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// A datastore's configuration changes on every diff, what moves on its own
// only with runtime fields, and a capture before schema 35 is not compared.
// The datastore is the nested lab's nfs-shared.
func TestDatastoreDetailChanges(t *testing.T) {
	yes, no := true, false
	threshold := int32(30)
	lab := func(edit func(*vsphere.DatastoreDetails)) *vsphere.DatastoreDetails {
		d := &vsphere.DatastoreDetails{ConfigStatus: "gray", Address: "192.168.150.1 /srv/vsphere", Hosts: []string{"192.168.150.11", "192.168.150.12"},
			VMIDs: []string{"vm-1007", "vm-1009"}, MultipleHostAccess: &yes, SIOCEnabled: &no, SIOCThreshold: &threshold}
		if edit != nil {
			edit(d)
		}
		return d
	}
	inPod := func(pct int32) *vsphere.DatastoreDetails {
		return lab(func(d *vsphere.DatastoreDetails) {
			d.StorageCluster = &vsphere.DatastoreCluster{ID: "group-p1", Name: "pod-a", CapacityBytes: ptrTo(int64(100)), FreeBytes: ptrTo(int64(50)),
				StorageDRS: &vsphere.StorageDRS{Enabled: true, SpaceThresholdMode: "utilization", SpaceUtilizationPct: pct}}
		})
	}
	for _, tc := range []struct {
		name          string
		before, after *vsphere.DatastoreDetails
		runtime       bool
		want          []FieldChange
	}{
		{"a host unmounted it", lab(nil), lab(func(d *vsphere.DatastoreDetails) { d.Hosts = d.Hosts[:1] }), false,
			[]FieldChange{{Field: "details.hosts", Before: `"192.168.150.11, 192.168.150.12"`, After: `"192.168.150.11"`}}},
		{"SIOC turned on", lab(nil), lab(func(d *vsphere.DatastoreDetails) { d.SIOCEnabled = &yes }), false,
			[]FieldChange{{Field: "details.sioc_enabled", Before: "false", After: "true"}}},
		{"a VM moved off it, without runtime fields", lab(nil), lab(func(d *vsphere.DatastoreDetails) { d.VMIDs = d.VMIDs[:1] }), false, nil},
		{"a VM moved off it, with runtime fields", lab(nil), lab(func(d *vsphere.DatastoreDetails) { d.VMIDs = d.VMIDs[:1] }), true,
			[]FieldChange{{Field: "details.vm_ids", Before: `"vm-1007, vm-1009"`, After: `"vm-1007"`}}},
		{"moved into a datastore cluster", lab(nil), lab(func(d *vsphere.DatastoreDetails) { d.StorageCluster = &vsphere.DatastoreCluster{Name: "pod-a"} }), false,
			nil}, // the cluster is recorded on one side only, so it is unknown rather than changed
		{"Storage DRS threshold raised", inPod(80), inPod(90), false,
			[]FieldChange{{Field: "details.storage_cluster.storage_drs.space_utilization_threshold_pct", Before: "80", After: "90"}}},
		{"the datastore cluster filled, without runtime fields", inPod(80), func() *vsphere.DatastoreDetails {
			d := inPod(80)
			d.StorageCluster.FreeBytes = ptrTo(int64(1))
			return d
		}(), false, nil},
		{"captured before schema 38", lab(func(d *vsphere.DatastoreDetails) { d.StorageCluster = &vsphere.DatastoreCluster{Name: "pod-a"} }), inPod(80), false, nil},
		{"captured before schema 35", nil, lab(nil), true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := json.Marshal(vsphere.Datastore{ID: "datastore-13", Name: "nfs-shared", Type: "NFS", Details: tc.before})
			after, _ := json.Marshal(vsphere.Datastore{ID: "datastore-13", Name: "nfs-shared", Type: "NFS", Details: tc.after})
			if got := changedResourceFields("datastore", before, after, tc.runtime); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("fields = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A path that goes dead, or a LUN the host no longer reaches, is reported on
// every diff. The LUN is esxi01's local-esxi01 disk; the second path is
// what a multipathed SAN LUN would add.
func TestHostMultipathChanges(t *testing.T) {
	const disk = "t10.ATA_____QEMU_HARDDISK___________________________esxi01local_________"
	lun := func(paths ...vsphere.HostMultipathPath) vsphere.HostMultipath {
		return vsphere.HostMultipath{LUN: "Local ATA Disk (" + disk + ")", Policy: "FIXED", PathCount: len(paths), Device: &vsphere.HostMultipathLUN{CanonicalName: disk, Paths: paths}}
	}
	a := vsphere.HostMultipathPath{Name: "vmhba0:C0:T2:L0", Adapter: "vmhba0", State: "active"}
	b := vsphere.HostMultipathPath{Name: "vmhba1:C0:T2:L0", Adapter: "vmhba1", State: "active"}
	// An import names a path by its adapter alone.
	imported := vsphere.HostMultipathPath{Name: "vmhba0", Adapter: "vmhba0", State: "active"}
	dead := b
	dead.State = "dead"
	boot := vsphere.HostMultipath{LUN: "boot", Policy: "FIXED"}
	for _, tc := range []struct {
		name          string
		before, after []vsphere.HostMultipath
		want          []FieldChange
	}{
		{"a path went dead", []vsphere.HostMultipath{lun(a, b)}, []vsphere.HostMultipath{lun(a, dead)},
			[]FieldChange{{Field: "multipaths[" + disk + "].paths", Before: "vmhba0 active, vmhba1 active", After: "vmhba0 active, vmhba1 dead"}}},
		{"a path was lost", []vsphere.HostMultipath{lun(a, b)}, []vsphere.HostMultipath{lun(a)},
			[]FieldChange{{Field: "multipaths[" + disk + "].paths", Before: "vmhba0 active, vmhba1 active", After: "vmhba0 active"}}},
		{"the same path as an import names it", []vsphere.HostMultipath{lun(a)}, []vsphere.HostMultipath{lun(imported)}, nil},
		{"a LUN is gone", []vsphere.HostMultipath{lun(a), boot}, []vsphere.HostMultipath{lun(a)},
			[]FieldChange{{Field: "multipaths", Before: "boot, " + disk, After: disk}}},
		{"policy changed", []vsphere.HostMultipath{lun(a)}, func() []vsphere.HostMultipath { m := lun(a); m.Policy = "RR"; return []vsphere.HostMultipath{m} }(),
			[]FieldChange{{Field: "multipaths[" + disk + "].policy", Before: "FIXED", After: "RR"}}},
		{"host configuration not collected", nil, []vsphere.HostMultipath{lun(a)}, nil},
		{"a capture predating the device keys the LUN by its device path",
			[]vsphere.HostMultipath{{LUN: "Local ATA Disk (" + disk + ")", DevicePath: "/vmfs/devices/disks/" + disk, Policy: "FIXED"}},
			[]vsphere.HostMultipath{lun(a)}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := json.Marshal(vsphere.Host{ID: "host-12", Name: "192.168.150.11", Multipaths: tc.before})
			after, _ := json.Marshal(vsphere.Host{ID: "host-12", Name: "192.168.150.11", Multipaths: tc.after})
			if got := changedResourceFields("host", before, after, false); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("fields = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func ptrTo[T any](v T) *T { return &v }
