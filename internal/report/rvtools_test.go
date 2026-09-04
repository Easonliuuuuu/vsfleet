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
	data := assessment.ExportData{
		Run:       assessment.Run{ID: 7, Label: "nightly", StartedAt: when, FinishedAt: when.Add(time.Minute), Status: assessment.RunComplete},
		Contexts:  []assessment.ContextRun{{Name: "prod", Endpoint: "https://vc.example", Datacenter: "dc-a", VCenterID: "vc-uuid", VMStatus: "success", Collections: []assessment.CollectionRun{{Kind: "host", Status: "success", ItemCount: 1}, {Kind: "cluster", Status: "empty"}, {Kind: "datastore", Status: "success", ItemCount: 1}}}},
		VMs:       []assessment.ExportVM{{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-uuid", VM: vsphere.VM{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "vm-1", Name: "app", PowerState: "poweredOn", CPU: 2, MemoryMB: 4096, StorageGB: 10, GuestOS: "Ubuntu", InstanceUUID: "instance", BIOSUUID: "bios", Host: "esx-1"}}, Snapshots: []vsphere.VMSnapshot{{ID: "snap-1", Name: "base", CreateTime: when, PowerState: "poweredOn", Quiesced: true}}}},
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
	wantSheets := []string{"vInfo", "vHost", "vCluster", "vDatastore", "vSnapshot", "vsfleetCoverage"}
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
	if got, _ := f.GetCellValue("vSnapshot", "E2"); got != "2026/01/02 03:04:05" {
		t.Fatalf("snapshot time=%q", got)
	}
	if got, _ := f.GetCellValue("vsfleetCoverage", "J2"); got != "vInfo" {
		t.Fatalf("coverage sheet=%q", got)
	}
}
