package rvimport

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// A datastore's vDatastore details import back as a vsfleet export wrote
// them, with the lab's local-esxi01 values. The workbook has the VM counts
// but not the VMs, so VMIDs read as not read, and the VMFS extents become
// the backing. A datastore exported before schema 35 has no details.
func TestDatastoreDetailsRoundTrip(t *testing.T) {
	no := false
	threshold := int32(30)
	const disk = "t10.ATA_____QEMU_HARDDISK___________________________esxi01local_________"
	capacity, free := int64(4<<30), int64(1<<30)
	for _, tc := range []struct {
		name    string
		details *vsphere.DatastoreDetails
		want    *vsphere.DatastoreDetails
		extents []string
	}{
		{"local-esxi01",
			&vsphere.DatastoreDetails{ConfigStatus: "gray", Address: disk, Hosts: []string{"192.168.150.11"}, VMIDs: []string{"vm-1008", "vm-1016"}, MultipleHostAccess: &no, SIOCEnabled: &no,
				SIOCThreshold: &threshold, VMFS: &vsphere.DatastoreVMFS{BlockSizeMB: 1, MaxBlocks: 63963136, Extents: 1, MajorVersion: 6, Version: "6.82"},
				StorageCluster: &vsphere.DatastoreCluster{Name: "pod-a", CapacityBytes: &capacity, FreeBytes: &free}},
			&vsphere.DatastoreDetails{ConfigStatus: "gray", Address: disk, Hosts: []string{"192.168.150.11"}, MultipleHostAccess: &no, SIOCEnabled: &no,
				SIOCThreshold: &threshold, VMFS: &vsphere.DatastoreVMFS{BlockSizeMB: 1, MaxBlocks: 63963136, Extents: 1, MajorVersion: 6, Version: "6.82"},
				StorageCluster: &vsphere.DatastoreCluster{Name: "pod-a", CapacityBytes: &capacity, FreeBytes: &free}},
			[]string{disk}},
		{"captured before schema 35", nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFixtureWorkbook(t, func(data *assessment.ExportData) {
				editDatastore(t, data, "ds-a1", func(d *vsphere.Datastore) { d.Details = tc.details })
			})
			_, run, store := importFixture(t, openFixture(t, path), Options{})
			resources, err := store.Resources(context.Background(), run.ID, kindDatastore)
			if err != nil {
				t.Fatal(err)
			}
			for _, resource := range resources {
				var d vsphere.Datastore
				if err := json.Unmarshal(resource.Payload, &d); err != nil {
					t.Fatal(err)
				}
				if d.ID != "ds-a1" {
					continue
				}
				if !reflect.DeepEqual(d.Details, tc.want) {
					t.Errorf("details = %+v, want %+v", d.Details, tc.want)
				}
				if !reflect.DeepEqual(d.Backing.Extents, tc.extents) {
					t.Errorf("extents = %v, want %v", d.Backing.Extents, tc.extents)
				}
				return
			}
			t.Fatal("ds-a1 was not imported")
		})
	}
}
