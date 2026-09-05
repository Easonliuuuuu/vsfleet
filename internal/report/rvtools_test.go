package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// rvtoolsTabOrder is the tab order both WriteRVTools and RVToolsCSV must
// produce.
var rvtoolsTabOrder = []string{"vInfo", "vCPU", "vMemory", "vDisk", "vPartition", "vNetwork", "vTools", "vHost", "vCluster", "vDatastore", "vSnapshot", "vsfleetCoverage"}

// sampleExportData builds one persisted run with a VM, its disk, NIC,
// guest partition, and snapshot, plus a host and datastore resource observation. Shared by the
// XLSX and CSV tests so both exercise identical evidence.
func sampleExportData(when time.Time) assessment.ExportData {
	hostPayload, _ := json.Marshal(vsphere.Host{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "host-1", Name: "esx-1", CPUCores: 8, CPUMHz: 2400, CPUUsageMHz: 1200, MemoryMB: 32768, MemoryUsageMB: 8192, VMCount: 4})
	datastorePayload, _ := json.Marshal(vsphere.Datastore{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "ds-1", Name: "datastore-1", CapacityBytes: 8 << 30, FreeBytes: 2 << 30, Accessible: true})
	thin := true
	connected := true
	return assessment.ExportData{
		Run:       assessment.Run{ID: 7, Label: "nightly", StartedAt: when, FinishedAt: when.Add(time.Minute), Status: assessment.RunComplete, InventorySchemaVersion: assessment.CurrentInventorySchemaVersion},
		Contexts:  []assessment.ContextRun{{Name: "prod", Endpoint: "https://vc.example", Datacenter: "dc-a", VCenterID: "vc-uuid", VMStatus: "success", Collections: []assessment.CollectionRun{{Kind: "host", Status: "success", ItemCount: 1}, {Kind: "cluster", Status: "empty"}, {Kind: "datastore", Status: "success", ItemCount: 1}}}},
		VMs:       []assessment.ExportVM{{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-uuid", VM: vsphere.VM{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "vm-1", Name: "app", PowerState: "poweredOn", CPU: 2, MemoryMB: 4096, StorageGB: 10, GuestOS: "Ubuntu", InstanceUUID: "instance", BIOSUUID: "bios", Host: "esx-1", ToolsState: "guestToolsRunning", ToolsVersion: "12352", ToolsVersionStatus: "guestToolsCurrent", Disks: []vsphere.VMDisk{{Key: 101, Label: "Hard disk 1", CapacityBytes: 8 << 30, UUID: "disk-uuid", ThinProvisioned: &thin, BackingPath: "[ds] app/app.vmdk"}}, NICs: []vsphere.VMNIC{{Key: 201, Label: "Network adapter 1", Network: "VM Network", Connected: &connected, IPv4: []string{"192.0.2.20"}}}, Partitions: []vsphere.VMPartition{{Path: "/", CapacityBytes: 8 << 30, FreeBytes: 2 << 30, FilesystemType: "ext4"}}}}, Snapshots: []vsphere.VMSnapshot{{ID: "snap-1", Name: "base", CreateTime: when, PowerState: "poweredOn", Quiesced: true}}}},
		Resources: []assessment.ResourceObservation{{Context: "prod", VCenterID: "vc-uuid", Kind: "host", ID: "host-1", Name: "esx-1", Payload: hostPayload}, {Context: "prod", VCenterID: "vc-uuid", Kind: "datastore", ID: "ds-1", Name: "datastore-1", Payload: datastorePayload}},
	}
}

func TestWriteRVToolsIsDeterministicAndComplete(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	data := sampleExportData(when)
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
	gotSheets := f.GetSheetList()
	if len(gotSheets) != len(rvtoolsTabOrder) {
		t.Fatalf("sheets=%v", gotSheets)
	}
	for i := range rvtoolsTabOrder {
		if gotSheets[i] != rvtoolsTabOrder[i] {
			t.Fatalf("sheets=%v", gotSheets)
		}
	}
	if got, _ := f.GetCellValue("vInfo", "A2"); got != "app" {
		t.Fatalf("vInfo A2=%q", got)
	}
	if got, _ := f.GetCellValue("vCPU", "D2"); got != "2" {
		t.Fatalf("vCPU CPUs=%q", got)
	}
	if got, _ := f.GetCellValue("vMemory", "D2"); got != "4096" {
		t.Fatalf("vMemory size=%q", got)
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
	if got, _ := f.GetCellValue("vTools", "D2"); got != "guestToolsRunning" {
		t.Fatalf("vTools state=%q", got)
	}
	if got, _ := f.GetCellValue("vTools", "E2"); got != "12352" {
		t.Fatalf("vTools version=%q", got)
	}
	if got, _ := f.GetCellValue("vTools", "F2"); got != "guestToolsCurrent" {
		t.Fatalf("vTools version status=%q", got)
	}
	if got, _ := f.GetCellValue("vSnapshot", "E2"); got != "2026/01/02 03:04:05" {
		t.Fatalf("snapshot time=%q", got)
	}
	if got, _ := f.GetCellValue("vsfleetCoverage", "J2"); got != "vInfo" {
		t.Fatalf("coverage sheet=%q", got)
	}
}

func TestRVToolsCSVMatchesXLSXAndIsDeterministic(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	data := sampleExportData(when)

	first, err := RVToolsCSV(data)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RVToolsCSV(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(rvtoolsTabOrder) {
		t.Fatalf("files=%d, want %d", len(first), len(rvtoolsTabOrder))
	}
	for i, name := range rvtoolsTabOrder {
		if first[i].Name != name+".csv" {
			t.Fatalf("file[%d]=%q, want %q", i, first[i].Name, name+".csv")
		}
		if !bytes.Equal(first[i].Data, second[i].Data) {
			t.Fatalf("%s differs between renders", first[i].Name)
		}
	}

	byName := make(map[string][]byte, len(first))
	for _, file := range first {
		byName[file.Name] = file.Data
	}

	vInfo := readCSV(t, byName["vInfo.csv"])
	if vInfo[0][0] != "VM" || vInfo[1][0] != "app" {
		t.Fatalf("vInfo.csv rows=%v", vInfo)
	}

	vTools := readCSV(t, byName["vTools.csv"])
	if got := vTools[1]; got[0] != "app" || got[3] != "guestToolsRunning" || got[4] != "12352" || got[5] != "guestToolsCurrent" {
		t.Fatalf("vTools.csv row=%v", got)
	}

	vSnapshot := readCSV(t, byName["vSnapshot.csv"])
	if got := vSnapshot[1][4]; got != "2026-01-02T03:04:05Z" {
		t.Fatalf("vSnapshot.csv timestamp=%q, want RFC3339", got)
	}
	if got := vSnapshot[1][5]; got != "true" {
		t.Fatalf("vSnapshot.csv quiesced=%q, want lowercase true", got)
	}

	vDisk := readCSV(t, byName["vDisk.csv"])
	if got := vDisk[1][6]; got != "8192" {
		t.Fatalf("vDisk.csv capacity=%q", got)
	}

	coverage := readCSV(t, byName["vsfleetCoverage.csv"])
	if got := coverage[1][9]; got != "vInfo" {
		t.Fatalf("vsfleetCoverage.csv sheet=%q", got)
	}
	if got := coverage[1][2]; got != "2026-01-02T03:04:05Z" {
		t.Fatalf("vsfleetCoverage.csv run started=%q, want RFC3339", got)
	}
}

func readCSV(t *testing.T, data []byte) [][]string {
	t.Helper()
	records, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	return records
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
	// Tab order is vInfo(2), vCPU(3), vMemory(4), vDisk(5), vPartition(6),
	// vNetwork(7), vTools(8), vHost(9), vCluster(10), vDatastore(11),
	// vSnapshot(12).
	for row, sheet := range map[string]string{"5": "vDisk", "7": "vNetwork"} {
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
	// vTools still has one row per VM on a pre-schema-3 run (the running
	// status predates the version columns), so its coverage row carries the
	// VM collection's own status with an explanatory message instead of
	// "not recorded".
	if got, _ := f.GetCellValue("vsfleetCoverage", "J8"); got != "vTools" {
		t.Fatalf("coverage sheet row 8=%q, want vTools", got)
	}
	if got, _ := f.GetCellValue("vsfleetCoverage", "K8"); got != "success" {
		t.Fatalf("vTools coverage status=%q", got)
	}
	if got, _ := f.GetCellValue("vsfleetCoverage", "L8"); got != "1" {
		t.Fatalf("vTools coverage count=%q", got)
	}
	if got, _ := f.GetCellValue("vsfleetCoverage", "M8"); got != "capture predates VMware Tools version inventory" {
		t.Fatalf("vTools coverage message=%q", got)
	}
}

func TestWriteRVToolsMarksToolsVersionGapForSchemaTwoRuns(t *testing.T) {
	data := assessment.ExportData{
		Run:      assessment.Run{ID: 9, StartedAt: time.Unix(0, 0).UTC(), FinishedAt: time.Unix(1, 0).UTC(), Status: assessment.RunComplete, InventorySchemaVersion: "2"},
		Contexts: []assessment.ContextRun{{Name: "prod", VMStatus: "success"}},
		VMs:      []assessment.ExportVM{{Observation: assessment.Observation{Context: "prod", VM: vsphere.VM{ID: "vm-1", Name: "app", ToolsState: "guestToolsRunning", Disks: []vsphere.VMDisk{{Key: 1}}, NICs: []vsphere.VMNIC{{Key: 2}}}}}},
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
	// Schema 2 already records devices, so vDisk has its real item count here
	// (unlike the "not recorded"+0 pairing schema 1 gets)...
	if got, _ := f.GetCellValue("vsfleetCoverage", "L5"); got != "1" {
		t.Fatalf("vDisk coverage count=%q, want 1 at schema 2", got)
	}
	// ...but the Tools version columns are still schema-3-only.
	if got, _ := f.GetCellValue("vTools", "D2"); got != "guestToolsRunning" {
		t.Fatalf("vTools state=%q", got)
	}
	if got, _ := f.GetCellValue("vTools", "E2"); got != "" {
		t.Fatalf("vTools version=%q, want empty at schema 2", got)
	}
	if got, _ := f.GetCellValue("vsfleetCoverage", "K8"); got != "success" {
		t.Fatalf("vTools coverage status=%q", got)
	}
	if got, _ := f.GetCellValue("vsfleetCoverage", "M8"); got != "capture predates VMware Tools version inventory" {
		t.Fatalf("vTools coverage message=%q", got)
	}
}

func TestWriteRVToolsDeviceCoverageMirrorsVMCollectionStatus(t *testing.T) {
	// vdisk/vnetwork are never persisted as their own collection kind, so the
	// coverage sheet has to read their status off the VM capture they ride on.
	for _, tc := range []struct {
		name           string
		context        assessment.ContextRun
		vms            []assessment.ExportVM
		status, detail string
		count          string
	}{
		{
			name:    "success",
			context: assessment.ContextRun{Name: "prod", VMStatus: "success"},
			vms:     []assessment.ExportVM{{Observation: assessment.Observation{Context: "prod", VM: vsphere.VM{ID: "vm-1", Name: "app", Disks: []vsphere.VMDisk{{Key: 1}}, NICs: []vsphere.VMNIC{{Key: 2}}}}}},
			status:  "success",
			count:   "1",
		},
		{
			name:    "failure carries the collection error",
			context: assessment.ContextRun{Name: "prod", VMStatus: "error", Error: "collect vms: permission denied"},
			status:  "error",
			detail:  "collect vms: permission denied",
			count:   "0",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := assessment.ExportData{
				Run:      assessment.Run{ID: 11, StartedAt: time.Unix(0, 0).UTC(), Status: assessment.RunComplete, InventorySchemaVersion: assessment.CurrentInventorySchemaVersion},
				Contexts: []assessment.ContextRun{tc.context},
				VMs:      tc.vms,
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
			// Coverage rows follow tab order: vDisk is row 5, vNetwork row 7
			// (vPartition sits between them).
			for row, sheet := range map[string]string{"5": "vDisk", "7": "vNetwork"} {
				if got, _ := f.GetCellValue("vsfleetCoverage", "J"+row); got != sheet {
					t.Fatalf("coverage sheet row %s=%q, want %q", row, got, sheet)
				}
				if got, _ := f.GetCellValue("vsfleetCoverage", "K"+row); got != tc.status {
					t.Fatalf("%s coverage status=%q, want %q", sheet, got, tc.status)
				}
				if got, _ := f.GetCellValue("vsfleetCoverage", "L"+row); got != tc.count {
					t.Fatalf("%s coverage count=%q, want %q", sheet, got, tc.count)
				}
				if got, _ := f.GetCellValue("vsfleetCoverage", "M"+row); got != tc.detail {
					t.Fatalf("%s coverage message=%q, want %q", sheet, got, tc.detail)
				}
			}
		})
	}
}

// partitionRun builds a one-context run whose VMs are given verbatim, so a
// test can say exactly which of them reported guest filesystems.
func partitionRun(schema string, vms []assessment.ExportVM) assessment.ExportData {
	return assessment.ExportData{
		Run:      assessment.Run{ID: 9, Status: assessment.RunComplete, InventorySchemaVersion: schema},
		Contexts: []assessment.ContextRun{{Name: "prod", Endpoint: "https://vc.example", VMStatus: "success"}},
		VMs:      vms,
	}
}

func partitionVM(name string, parts ...vsphere.VMPartition) assessment.ExportVM {
	return assessment.ExportVM{Observation: assessment.Observation{Context: "prod", VM: vsphere.VM{ID: name, Name: name, Partitions: parts}}}
}

func TestWriteRVToolsRendersGuestPartitions(t *testing.T) {
	data := partitionRun(assessment.CurrentInventorySchemaVersion, []assessment.ExportVM{
		partitionVM("app", vsphere.VMPartition{Path: "/", DiskKeys: []int32{2000}, CapacityBytes: 8 << 30, FreeBytes: 2 << 30, FilesystemType: "ext4"}),
	})
	var out bytes.Buffer
	if err := WriteRVTools(&out, data); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// 8 GiB capacity with 2 GiB free is 6 GiB consumed and 25% free.
	for cell, want := range map[string]string{
		"A2": "app", "D2": "2000", "E2": "/", "F2": "8192", "G2": "6144", "H2": "2048", "I2": "25", "J2": "ext4",
	} {
		if got, _ := f.GetCellValue("vPartition", cell); got != want {
			t.Errorf("vPartition!%s=%q, want %q", cell, got, want)
		}
	}
}

// Disk Key is the column that makes vPartition joinable to vDisk, so what it
// renders for each of the three cases Tools can produce is the whole point of
// having it: nothing before vSphere 7.0, one key normally, and several for a
// volume spanning disks.
func TestWriteRVToolsRendersPartitionDiskKeys(t *testing.T) {
	data := partitionRun(assessment.CurrentInventorySchemaVersion, []assessment.ExportVM{
		partitionVM("mapped", vsphere.VMPartition{Path: "/", DiskKeys: []int32{2000}, CapacityBytes: 1 << 30}),
		partitionVM("spanned", vsphere.VMPartition{Path: "/data", DiskKeys: []int32{2000, 2001}, CapacityBytes: 1 << 30}),
		partitionVM("unmapped", vsphere.VMPartition{Path: "/", CapacityBytes: 1 << 30}),
	})
	var out bytes.Buffer
	if err := WriteRVTools(&out, data); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// A lone key stays a number so a spreadsheet matches it against vDisk's
	// own numeric Disk Key; an estate too old to report the mapping leaves the
	// cell empty rather than claiming disk key zero.
	for cell, want := range map[string]string{"D2": "2000", "D3": "2000, 2001", "D4": ""} {
		if got, _ := f.GetCellValue("vPartition", cell); got != want {
			t.Errorf("vPartition!%s=%q, want %q", cell, got, want)
		}
	}
}

// A capacity vSphere could not size must not be reported as a full disk: the
// column is summed downstream, and 0% free reads as an alarm.
func TestWriteRVToolsHandlesUnsizedPartitions(t *testing.T) {
	data := partitionRun(assessment.CurrentInventorySchemaVersion, []assessment.ExportVM{
		partitionVM("app", vsphere.VMPartition{Path: "/mnt/unsized"}),
		// Tools occasionally reports free space exceeding capacity; consumed
		// must not go negative.
		partitionVM("web", vsphere.VMPartition{Path: "C:\\", CapacityBytes: 1 << 30, FreeBytes: 2 << 30}),
	})
	var out bytes.Buffer
	if err := WriteRVTools(&out, data); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for cell, want := range map[string]string{"G2": "0", "I2": "0", "G3": "0"} {
		if got, _ := f.GetCellValue("vPartition", cell); got != want {
			t.Errorf("vPartition!%s=%q, want %q", cell, got, want)
		}
	}
}

// The point of the coverage sheet: a short vPartition tab must be
// distinguishable from a small estate. Partitions come from VMware Tools, so
// a fully successful capture can still cover only part of the estate.
func TestWriteRVToolsReportsPartialGuestPartitionCoverage(t *testing.T) {
	withParts := partitionVM("app", vsphere.VMPartition{Path: "/", CapacityBytes: 1 << 30, FreeBytes: 1 << 29})
	noParts := partitionVM("no-tools")

	for _, tc := range []struct {
		name           string
		schema         string
		vms            []assessment.ExportVM
		status, detail string
	}{
		{
			name: "every VM answered", schema: assessment.CurrentInventorySchemaVersion,
			vms: []assessment.ExportVM{withParts}, status: "success", detail: "",
		},
		{
			name: "some VMs had no running Tools", schema: assessment.CurrentInventorySchemaVersion,
			vms: []assessment.ExportVM{withParts, noParts}, status: "partial",
			detail: "1 of 2 VMs reported guest filesystems; the rest had no running VMware Tools",
		},
		{
			name: "no VM answered", schema: assessment.CurrentInventorySchemaVersion,
			vms: []assessment.ExportVM{noParts}, status: "partial",
			detail: "no VM reported guest filesystems; VMware Tools must be running",
		},
		{
			name: "capture predates the tab", schema: "3",
			vms: []assessment.ExportVM{withParts}, status: "not recorded",
			detail: "capture predates guest partition inventory",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := WriteRVTools(&out, partitionRun(tc.schema, tc.vms)); err != nil {
				t.Fatal(err)
			}
			f, err := excelize.OpenReader(bytes.NewReader(out.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			// vPartition is the sixth tab, so row 6 of the coverage sheet.
			if got, _ := f.GetCellValue("vsfleetCoverage", "J6"); got != "vPartition" {
				t.Fatalf("coverage row 6=%q, want vPartition", got)
			}
			if got, _ := f.GetCellValue("vsfleetCoverage", "K6"); got != tc.status {
				t.Errorf("status=%q, want %q", got, tc.status)
			}
			if got, _ := f.GetCellValue("vsfleetCoverage", "M6"); got != tc.detail {
				t.Errorf("detail=%q, want %q", got, tc.detail)
			}
		})
	}
}
