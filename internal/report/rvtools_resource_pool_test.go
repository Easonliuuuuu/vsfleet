package report

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// Cluster-Lab's root pool in the nested lab holds seven VMs, one of them
// powered on (vcsa, 2 vCPUs); the real RVTools 4.8.2.1 export of 2026-11-05
// writes it as below. Three of the seven stand in for the lab's VMs here.
func TestWriteRVToolsResourcePoolCounts(t *testing.T) {
	pool := vsphere.ResourcePool{Location: vsphere.Location{Datacenter: "DC-Lab", Path: "/DC-Lab/host/Cluster-Lab/Resources"}, ID: "resgroup-10",
		Name: "Resources", Root: true, Status: "green", VMRefs: []string{"vm-vcsa", "vm-off", "vm-tpl"}}
	vms := []vsphere.VM{
		{ID: "vm-vcsa", Name: "vcsa", PowerState: "poweredOn", CPU: 2},
		{ID: "vm-off", Name: "vm-cpu-topology", PowerState: "poweredOff", CPU: 4},
		{ID: "vm-tpl", Name: "tpl", PowerState: "poweredOff", IsTemplate: true, CPU: 1},
	}
	export := func(vmStatus string) map[string]string {
		data := sampleExportData(time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC))
		data.Contexts[0].VMStatus = vmStatus
		data.VMs = nil
		for _, vm := range vms {
			data.VMs = append(data.VMs, assessment.ExportVM{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-uuid", VM: vm}})
		}
		payload, err := json.Marshal(pool)
		if err != nil {
			t.Fatal(err)
		}
		for i := range data.Resources {
			if data.Resources[i].Kind == "resourcepool" {
				data.Resources[i].Payload = payload
			}
		}
		return firstRowByHeader(t, data, "vRP")
	}
	for _, tc := range []struct {
		name     string
		vmStatus string
		want     map[string]string
	}{
		{"lab", "success", map[string]string{"Resource Pool name": "Resources", "Resource Pool path": "/DC-Lab/Cluster-Lab/Resources",
			"# VMs total": "3", "# VMs": "1", "# vCPUs": "2", "vCPUs": "7"}},
		// Without every VM, the powered-on counts would undercount.
		{"VM collection incomplete", "partial", map[string]string{"# VMs total": "3", "# VMs": "", "# vCPUs": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkCells(t, export(tc.vmStatus), tc.want)
		})
	}
}

// Every column the profile leaves out must be one it really does not write,
// on a worksheet it does write, and say why. A column exported later but
// left on the list would make explain rvtools contradict the workbook.
func TestOmittedColumnsAreNotWritten(t *testing.T) {
	profile, err := Profile()
	if err != nil {
		t.Fatal(err)
	}
	written := map[string]map[string]bool{}
	for _, sheet := range profile {
		written[sheet.Name] = map[string]bool{}
		for _, c := range sheet.Columns {
			written[sheet.Name][c.Name] = true
		}
	}
	for sheet, omitted := range omittedColumns {
		if written[sheet] == nil {
			t.Errorf("%s lists omitted columns but is not a worksheet the profile writes", sheet)
		}
		for _, c := range omitted {
			if written[sheet][c.Name] {
				t.Errorf("%s/%s is both written and listed as left out", sheet, c.Name)
			}
			if c.Reason == "" {
				t.Errorf("%s/%s is left out without a reason", sheet, c.Name)
			}
		}
	}
	for _, sheet := range OmittedSheets() {
		if written[sheet.Name] != nil {
			t.Errorf("%s is listed as not written but the profile writes it", sheet.Name)
		}
		if !sheet.NotWritten || sheet.Note == "" || len(sheet.Omitted) == 0 {
			t.Errorf("%s must be marked not written, with a reason and its columns: %+v", sheet.Name, sheet)
		}
	}
}
