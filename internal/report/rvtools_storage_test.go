package report

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

const labLocalDisk = "t10.ATA_____QEMU_HARDDISK___________________________esxi01local_________"

// labStorage is the nested lab's esxi01 with its local-esxi01 and boot
// disks, the local-esxi01 and nfs-shared datastores, vcsa (on, on
// nfs-shared), two powered-off VMs on local-esxi01 and the tpl-normal
// template on nfs-shared.
func labStorage(t *testing.T, withDetails bool) assessment.ExportData {
	t.Helper()
	yes, no := true, false
	threshold, depth, level := int32(30), int32(31), int32(5)
	local := vsphere.HostMultipath{LUN: "Local ATA Disk (" + labLocalDisk + ")", DevicePath: "/vmfs/devices/disks/" + labLocalDisk, Policy: "FIXED", LocalDisk: &yes,
		PathCount: 1, Active: 1, WorkingPaths: 1}
	boot := vsphere.HostMultipath{LUN: "Local ATA Disk (t10.ATA_____QEMU_HARDDISK___________________________esxi01boot__________)",
		DevicePath: "/vmfs/devices/disks/t10.ATA_____QEMU_HARDDISK___________________________esxi01boot__________", Policy: "FIXED", LocalDisk: &yes, PathCount: 1, Active: 1, WorkingPaths: 1}
	localDS := vsphere.Datastore{Location: vsphere.Location{Datacenter: "DC-Lab"}, ID: "datastore-14", Name: "local-esxi01", Type: "VMFS", Accessible: true,
		CapacityBytes: 32512 << 20, FreeBytes: 31068 << 20, Backing: vsphere.DatastoreBacking{Extents: []string{labLocalDisk}}}
	nfs := vsphere.Datastore{Location: vsphere.Location{Datacenter: "DC-Lab"}, ID: "datastore-13", Name: "nfs-shared", Type: "NFS", Accessible: true, CapacityBytes: 300231 << 20, FreeBytes: 56343 << 20}
	if withDetails {
		local.Device = &vsphere.HostMultipathLUN{CanonicalName: labLocalDisk, OperationalState: "ok", VStorageSupport: "vStorageUnsupported", QueueDepth: &depth,
			Vendor: "ATA     ", Model: "QEMU HARDDISK   ", Revision: "2.5+", SCSILevel: &level, Serial: "esxi01local         ", UUID: "01000000006573786930316c6f63616c20202020202020202051454d552048",
			Paths: []vsphere.HostMultipathPath{{Name: "vmhba0:C0:T2:L0", Adapter: "vmhba0", State: "active"}}}
		localDS.Details = &vsphere.DatastoreDetails{ConfigStatus: "gray", Address: labLocalDisk, Hosts: []string{"192.168.150.11"}, VMIDs: []string{"vm-1008", "vm-1016"},
			MultipleHostAccess: &no, SIOCEnabled: &no, SIOCThreshold: &threshold, VMFS: &vsphere.DatastoreVMFS{BlockSizeMB: 1, MaxBlocks: 63963136, Extents: 1, MajorVersion: 6, Version: "6.82"}}
		nfs.Details = &vsphere.DatastoreDetails{ConfigStatus: "gray", Address: "192.168.150.1 /srv/vsphere", Hosts: []string{"192.168.150.11", "192.168.150.12"}, VMIDs: []string{"vm-1007", "vm-1010"},
			MultipleHostAccess: &yes, SIOCEnabled: &no, SIOCThreshold: &threshold}
	}
	host := vsphere.Host{Location: vsphere.Location{Datacenter: "DC-Lab"}, ID: "host-12", Name: "192.168.150.11", Cluster: "Cluster-Lab", Multipaths: []vsphere.HostMultipath{boot, local}}
	resource := func(kind, id, name string, v any) assessment.ResourceObservation {
		payload, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return assessment.ResourceObservation{Context: "lab", VCenterID: "vc-uuid", Kind: kind, ID: id, Name: name, Payload: payload}
	}
	vm := func(id, power string) assessment.ExportVM {
		return assessment.ExportVM{Observation: assessment.Observation{Context: "lab", VCenterID: "vc-uuid", VM: vsphere.VM{ID: id, Name: id, PowerState: power}}}
	}
	// tpl-normal is a template on nfs-shared, which RVTools does not count.
	template := vm("vm-1010", "poweredOff")
	template.Observation.VM.IsTemplate = true
	return assessment.ExportData{
		Run:       assessment.Run{ID: 1, InventorySchemaVersion: assessment.CurrentInventorySchemaVersion},
		Contexts:  []assessment.ContextRun{{Name: "lab", Endpoint: "https://192.168.150.10", VCenterID: "vc-uuid", VMStatus: "success", Collections: []assessment.CollectionRun{{Kind: "host", Status: "success"}, {Kind: "datastore", Status: "success"}}}},
		VMs:       []assessment.ExportVM{vm("vm-1007", "poweredOn"), vm("vm-1008", "poweredOff"), vm("vm-1016", "poweredOff"), template},
		Resources: []assessment.ResourceObservation{resource("host", host.ID, host.Name, host), resource("datastore", localDS.ID, localDS.Name, localDS), resource("datastore", nfs.ID, nfs.Name, nfs)},
	}
}

// rowsByHeader renders the workbook and returns each data row of sheet as a
// header-to-cell map.
func rowsByHeader(t *testing.T, data assessment.ExportData, sheet string) []map[string]string {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteRVToolsWith(&buf, data, healthReport(data), ExportOptions{}); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := f.GetRows(sheet)
	if err != nil || len(rows) == 0 {
		t.Fatalf("%s rows=%v err=%v", sheet, rows, err)
	}
	var out []map[string]string
	for _, row := range rows[1:] {
		m := make(map[string]string, len(rows[0]))
		for i, h := range rows[0] {
			if i < len(row) {
				m[h] = row[i]
			} else {
				m[h] = ""
			}
		}
		out = append(out, m)
	}
	return out
}

// The cells are what the real RVTools 4.8.2.1 export of the lab writes for
// local-esxi01 and nfs-shared, NFS's zero VMFS sizes included. A capture
// before schema 35 leaves them empty.
func TestWriteRVToolsDatastoreDetails(t *testing.T) {
	headers := []string{"Config status", "Address", "# VMs total", "# VMs", "SIOC enabled", "SIOC Threshold", "# Hosts", "Hosts", "Cluster name",
		"Block size", "Max Blocks", "# Extents", "Major Version", "Version", "VMFS Upgradeable", "MHA"}
	for _, tc := range []struct {
		name    string
		details bool
		want    map[string][]string
	}{
		{"lab", true, map[string][]string{
			"local-esxi01": {"gray", labLocalDisk, "2", "0", "FALSE", "30", "1", "192.168.150.11", "", "1", "63963136", "1", "6", "6.82", "FALSE", "FALSE"},
			"nfs-shared":   {"gray", "192.168.150.1 /srv/vsphere", "1", "1", "FALSE", "30", "2", "192.168.150.11, 192.168.150.12", "", "0", "0", "0", "0", "", "", "TRUE"},
		}},
		{"captured before schema 35", false, map[string][]string{
			"local-esxi01": make([]string, 16),
			"nfs-shared":   make([]string, 16),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := rowsByHeader(t, labStorage(t, tc.details), "vDatastore")
			if len(rows) != 2 {
				t.Fatalf("rows = %d, want 2", len(rows))
			}
			for _, row := range rows {
				want := tc.want[row["Name"]]
				for i, h := range headers {
					if row[h] != want[i] {
						t.Errorf("%s %s = %q, want %q", row["Name"], h, row[h], want[i])
					}
				}
			}
		})
	}
}

// vMultiPath lists the LUN local-esxi01 is on, as RVTools does, and not
// esxi01's boot disk, which no datastore is on. Object ID names the
// datastore. A capture before schema 35 still finds the datastore's LUN by
// its device path and leaves the device cells empty.
func TestWriteRVToolsMultipathRowsFollowTheDatastores(t *testing.T) {
	for _, tc := range []struct {
		name    string
		details bool
		want    map[string]string
	}{
		{"lab", true, map[string]string{"Host": "192.168.150.11", "Cluster": "Cluster-Lab", "Datacenter": "DC-Lab", "Datastore": "local-esxi01", "Disk": labLocalDisk,
			"Display name": "Local ATA Disk (" + labLocalDisk + ")", "Policy": "FIXED", "Oper. State": "ok", "Path 1": "vmhba0", "Path 1 state": "active", "Path 2": "",
			"vStorage": "vStorageUnsupported", "Queue depth": "31", "Vendor": "ATA     ", "Model": "QEMU HARDDISK   ", "Revision": "2.5+", "Level": "5",
			"Serial #": "esxi01local         ", "UUID": "01000000006573786930316c6f63616c20202020202020202051454d552048", "Object ID": "datastore-14", "Path count": "1"}},
		{"captured before schema 35", false, map[string]string{"Host": "192.168.150.11", "Datastore": "local-esxi01", "Disk": labLocalDisk, "Policy": "FIXED",
			"Oper. State": "", "Path 1": "", "Vendor": "", "Object ID": "datastore-14", "Path count": "1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := rowsByHeader(t, labStorage(t, tc.details), "vMultiPath")
			if len(rows) != 1 {
				t.Fatalf("rows = %+v, want only local-esxi01's LUN", rows)
			}
			for h, want := range tc.want {
				if rows[0][h] != want {
					t.Errorf("%s = %q, want %q", h, rows[0][h], want)
				}
			}
		})
	}
}
