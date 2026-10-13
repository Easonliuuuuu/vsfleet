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

// labPlacementData mirrors the nested lab: two clusters, a vApp in
// Cluster-DR, a template and the discovered vcsa. The expected cells in
// TestWriteRVToolsVInfoMatchesRVToolsPlacement are the real RVTools 4.8.1.4
// export of that lab.
func labPlacementData(t *testing.T) assessment.ExportData {
	t.Helper()
	at := vsphere.Location{Context: "lab", Datacenter: "DC-Lab"}
	payload := func(v any) json.RawMessage {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	resource := func(kind, id, name string, v any) assessment.ResourceObservation {
		return assessment.ResourceObservation{Context: "lab", VCenterID: "vc-uuid", Kind: kind, ID: id, Name: name, Payload: payload(v)}
	}
	secureOn, secureOff := true, false
	vm := func(id, name, folder string, mutate func(*vsphere.VM)) assessment.ExportVM {
		v := vsphere.VM{Location: at, ID: id, Name: name, Folder: folder, PowerState: "poweredOff", ConnectionState: "connected",
			ConfigurationAvailable: true, Firmware: "bios", SecureBootEnabled: &secureOff,
			Disks: []vsphere.VMDisk{{Key: 2000}}, NICs: []vsphere.VMNIC{{Key: 4000}}}
		if mutate != nil {
			mutate(&v)
		}
		return assessment.ExportVM{Observation: assessment.Observation{Context: "lab", VCenterID: "vc-uuid", VM: v}}
	}
	return assessment.ExportData{
		Run: assessment.Run{ID: 1, StartedAt: time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC), Status: assessment.RunComplete, InventorySchemaVersion: assessment.CurrentInventorySchemaVersion},
		Contexts: []assessment.ContextRun{{Name: "lab", Endpoint: "https://192.168.150.10", Datacenter: "DC-Lab", VCenterID: "vc-uuid", VMStatus: "success",
			Collections: []assessment.CollectionRun{{Kind: "resourcepool", Status: "success", ItemCount: 4}, {Kind: "vapp", Status: "success", ItemCount: 2}}}},
		VMs: []assessment.ExportVM{
			vm("vm-1", "vm-normal", "/", nil),
			vm("vm-2", "vm-uefi-secure", "/", func(v *vsphere.VM) { v.Firmware, v.SecureBootEnabled = "efi", &secureOn }),
			vm("vm-3", "vcsa", "/Discovered virtual machine", func(v *vsphere.VM) {
				v.GuestHostName, v.PowerState = "localhost", "poweredOn"
				v.Disks = make([]vsphere.VMDisk, 17)
			}),
			vm("vm-4", "tpl-normal", "/", func(v *vsphere.VM) { v.IsTemplate = true }),
			vm("vm-5", "vapp-db", "/vapp-lab", nil),
			vm("vm-6", "in-ambiguous-vapp", "/vapp-dup", nil),
			vm("vm-7", "summary-only", "/", func(v *vsphere.VM) {
				v.ConfigurationAvailable, v.Firmware, v.SecureBootEnabled, v.Disks, v.NICs = false, "", nil, nil, nil
			}),
		},
		Resources: []assessment.ResourceObservation{
			resource("resourcepool", "resgroup-1", "Resources", vsphere.ResourcePool{Location: vsphere.Location{Context: "lab", Datacenter: "DC-Lab", Path: "/DC-Lab/host/Cluster-DR/Resources"},
				ID: "resgroup-1", Name: "Resources", Parent: "Cluster-DR", VMRefs: []string{"vm-1", "vm-7"}}),
			resource("resourcepool", "resgroup-2", "Resources", vsphere.ResourcePool{Location: vsphere.Location{Context: "lab", Datacenter: "DC-Lab", Path: "/DC-Lab/host/Cluster-Lab/Resources"},
				ID: "resgroup-2", Name: "Resources", Parent: "Cluster-Lab", VMRefs: []string{"vm-2", "vm-3"}}),
			// Two pools named "shared" in Cluster-Lab: a vApp below one of
			// them cannot be placed from its parent's name alone.
			resource("resourcepool", "resgroup-3", "shared", vsphere.ResourcePool{Location: vsphere.Location{Context: "lab", Datacenter: "DC-Lab", Path: "/DC-Lab/host/Cluster-Lab/Resources/a/shared"},
				ID: "resgroup-3", Name: "shared", Parent: "a"}),
			resource("resourcepool", "resgroup-4", "shared", vsphere.ResourcePool{Location: vsphere.Location{Context: "lab", Datacenter: "DC-Lab", Path: "/DC-Lab/host/Cluster-Lab/Resources/b/shared"},
				ID: "resgroup-4", Name: "shared", Parent: "b"}),
			resource("vapp", "resgroup-v1", "vapp-lab", vsphere.VApp{Location: vsphere.Location{Context: "lab", Datacenter: "DC-Lab", Path: "/DC-Lab/vm/vapp-lab"},
				ID: "resgroup-v1", Name: "vapp-lab", ParentContainer: "Resources", ComputeResource: "Cluster-DR", DirectVMRefs: []string{"VirtualMachine:vm-5"}}),
			resource("vapp", "resgroup-v2", "vapp-dup", vsphere.VApp{Location: vsphere.Location{Context: "lab", Datacenter: "DC-Lab", Path: "/DC-Lab/vm/vapp-dup"},
				ID: "resgroup-v2", Name: "vapp-dup", ParentContainer: "shared", ComputeResource: "Cluster-Lab", DirectVMRefs: []string{"VirtualMachine:vm-6"}}),
		},
	}
}

func TestWriteRVToolsVInfoMatchesRVToolsPlacement(t *testing.T) {
	data := labPlacementData(t)
	var buf bytes.Buffer
	if err := WriteRVTools(&buf, data, healthReport(data)); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	columns := []string{"DNS Name", "Connection state", "NICs", "Disks", "Resource pool", "Folder", "vApp", "EFI Secure boot", "Firmware", "VI SDK Server"}
	for vm, want := range map[string][]string{
		"vm-normal":         {"", "connected", "1", "1", "/DC-Lab/Cluster-DR/Resources", "/DC-Lab", "", "FALSE", "bios", "192.168.150.10"},
		"vm-uefi-secure":    {"", "connected", "1", "1", "/DC-Lab/Cluster-Lab/Resources", "/DC-Lab", "", "TRUE", "efi", "192.168.150.10"},
		"vcsa":              {"localhost", "connected", "1", "17", "/DC-Lab/Cluster-Lab/Resources", "/DC-Lab/Discovered virtual machine", "", "FALSE", "bios", "192.168.150.10"},
		"tpl-normal":        {"", "connected", "1", "1", "", "/DC-Lab", "", "FALSE", "bios", "192.168.150.10"},
		"vapp-db":           {"", "connected", "1", "1", "/DC-Lab/Cluster-DR/Resources/vapp-lab", "", "vapp-lab", "FALSE", "bios", "192.168.150.10"},
		"in-ambiguous-vapp": {"", "connected", "1", "1", "", "", "vapp-dup", "FALSE", "bios", "192.168.150.10"},
		"summary-only":      {"", "connected", "", "", "/DC-Lab/Cluster-DR/Resources", "/DC-Lab", "", "", "", "192.168.150.10"},
	} {
		t.Run(vm, func(t *testing.T) {
			got := sheetCells(t, f, "vInfo", vm, columns)
			for i, column := range columns {
				if got[i] != want[i] {
					t.Errorf("%s = %q, want %q", column, got[i], want[i])
				}
			}
		})
	}

	// Every per-VM sheet places a vApp member by its vApp, not a folder.
	for _, sheet := range []string{"vCPU", "vMemory", "vTools", "vDisk", "vNetwork"} {
		if got := sheetCells(t, f, sheet, "vapp-db", []string{"Folder"})[0]; got != "" {
			t.Errorf("%s Folder for a vApp member = %q, want empty", sheet, got)
		}
		if got := sheetCells(t, f, sheet, "vcsa", []string{"Folder"})[0]; got != "/DC-Lab/Discovered virtual machine" {
			t.Errorf("%s Folder = %q, want the datacenter-rooted path", sheet, got)
		}
	}
}

// sheetCells returns the named columns of the first row whose first cell is
// key.
func sheetCells(t *testing.T, f *excelize.File, sheet, key string, columns []string) []string {
	t.Helper()
	rows, err := f.GetRows(sheet)
	if err != nil || len(rows) == 0 {
		t.Fatalf("%s rows=%v err=%v", sheet, rows, err)
	}
	for _, row := range rows[1:] {
		if len(row) == 0 || row[0] != key {
			continue
		}
		out := make([]string, len(columns))
		for i, column := range columns {
			found := false
			for j, header := range rows[0] {
				if header == column {
					found = true
					if j < len(row) {
						out[i] = row[j]
					}
				}
			}
			if !found {
				t.Fatalf("%s has no %q column", sheet, column)
			}
		}
		return out
	}
	t.Fatalf("%s has no row for %q", sheet, key)
	return nil
}
