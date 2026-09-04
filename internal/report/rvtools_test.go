package report

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestWriteRVToolsIsDeterministicAndComplete(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	hostPayload, _ := json.Marshal(vsphere.Host{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "host-1", Name: "esx-1", CPUCores: 8, CPUMHz: 2400, CPUUsageMHz: 1200, MemoryMB: 32768, MemoryUsageMB: 8192, VMCount: 4})
	datastorePayload, _ := json.Marshal(vsphere.Datastore{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "ds-1", Name: "datastore-1", CapacityBytes: 8 << 30, FreeBytes: 2 << 30, Accessible: true})
	thin := true
	connected := true
	data := assessment.ExportData{
		Run:       assessment.Run{ID: 7, Label: "nightly", StartedAt: when, FinishedAt: when.Add(time.Minute), Status: assessment.RunComplete, InventorySchemaVersion: assessment.CurrentInventorySchemaVersion},
		Contexts:  []assessment.ContextRun{{Name: "prod", Endpoint: "https://vc.example", Datacenter: "dc-a", VCenterID: "vc-uuid", VMStatus: "success", Collections: []assessment.CollectionRun{{Kind: "host", Status: "success", ItemCount: 1}, {Kind: "cluster", Status: "empty"}, {Kind: "datastore", Status: "success", ItemCount: 1}}}},
		VMs:       []assessment.ExportVM{{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-uuid", VM: vsphere.VM{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "vm-1", Name: "app", PowerState: "poweredOn", CPU: 2, MemoryMB: 4096, StorageGB: 10, GuestOS: "Ubuntu", InstanceUUID: "instance", BIOSUUID: "bios", Host: "esx-1", Disks: []vsphere.VMDisk{{Key: 101, Label: "Hard disk 1", CapacityBytes: 8 << 30, UUID: "disk-uuid", ThinProvisioned: &thin, BackingPath: "[ds] app/app.vmdk"}}, NICs: []vsphere.VMNIC{{Key: 201, Label: "Network adapter 1", Network: "VM Network", Connected: &connected, IPv4: []string{"192.0.2.20"}}}}}, Snapshots: []vsphere.VMSnapshot{{ID: "snap-1", Name: "base", CreateTime: when, PowerState: "poweredOn", Quiesced: true}}}},
		Resources: []assessment.ResourceObservation{{Context: "prod", VCenterID: "vc-uuid", Kind: "host", ID: "host-1", Name: "esx-1", Payload: hostPayload}, {Context: "prod", VCenterID: "vc-uuid", Kind: "datastore", ID: "ds-1", Name: "datastore-1", Payload: datastorePayload}},
	}
	var first, second bytes.Buffer
	if err := WriteRVTools(&first, data); err != nil {
		t.Fatal(err)
	}
	if err := WriteRVTools(&second, data); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("repeated exports differ")
	}
	f, err := excelize.OpenReader(bytes.NewReader(first.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	wantSheets := []string{"vInfo", "vDisk", "vNetwork", "vHost", "vCluster", "vDatastore", "vSnapshot", "vsfleetCoverage"}
	gotSheets := f.GetSheetList()
	if len(gotSheets) != len(wantSheets) {
		t.Fatalf("sheets=%v", gotSheets)
	}
	for i := range wantSheets {
		if gotSheets[i] != wantSheets[i] {
			t.Fatalf("sheets=%v", gotSheets)
		}
	}
	if got, _ := f.GetCellValue("vInfo", "A2"); got != "app" {
		t.Fatalf("vInfo A2=%q", got)
	}
	if got, _ := f.GetCellValue("vDisk", "A2"); got != "app" {
		t.Fatalf("vDisk A2=%q", got)
	}
	if got, _ := f.GetCellValue("vDisk", "G2"); got != "8192" {
		t.Fatalf("vDisk capacity=%q", got)
	}
	if got, _ := f.GetCellValue("vNetwork", "F2"); got != "VM Network" {
		t.Fatalf("vNetwork network=%q", got)
	}
	if got, _ := f.GetCellValue("vNetwork", "K2"); got != "192.0.2.20" {
		t.Fatalf("vNetwork ipv4=%q", got)
	}
	if got, _ := f.GetCellValue("vSnapshot", "E2"); got != "2026/01/02 03:04:05" {
		t.Fatalf("snapshot time=%q", got)
	}
	if got, _ := f.GetCellValue("vsfleetCoverage", "J2"); got != "vInfo" {
		t.Fatalf("coverage sheet=%q", got)
	}
}

func TestWriteRVToolsMarksDeviceTabsNotRecordedForOldRuns(t *testing.T) {
	data := assessment.ExportData{
		Run:      assessment.Run{ID: 8, StartedAt: time.Unix(0, 0).UTC(), FinishedAt: time.Unix(1, 0).UTC(), Status: assessment.RunComplete, InventorySchemaVersion: "1"},
		Contexts: []assessment.ContextRun{{Name: "prod", VMStatus: "success"}},
		VMs:      []assessment.ExportVM{{Observation: assessment.Observation{Context: "prod", VM: vsphere.VM{ID: "vm-1", Name: "app", Disks: []vsphere.VMDisk{{Key: 1}}, NICs: []vsphere.VMNIC{{Key: 2}}}}}},
	}
	var output bytes.Buffer
	if err := WriteRVTools(&output, data); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for row, sheet := range map[string]string{"3": "vDisk", "4": "vNetwork"} {
		if got, _ := f.GetCellValue("vsfleetCoverage", "J"+row); got != sheet {
			t.Fatalf("coverage sheet row %s=%q, want %q", row, got, sheet)
		}
		if got, _ := f.GetCellValue("vsfleetCoverage", "K"+row); got != "not recorded" {
			t.Fatalf("coverage status row %s=%q", row, got)
		}
		if got, _ := f.GetCellValue("vsfleetCoverage", "L"+row); got != "0" {
			t.Fatalf("coverage count row %s=%q", row, got)
		}
	}
}
