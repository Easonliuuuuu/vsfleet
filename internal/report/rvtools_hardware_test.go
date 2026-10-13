package report

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The "current" row is what the real RVTools 4.8.1.4 export of the nested lab
// writes for a version-21 VM with no scheduled upgrade.
func TestWriteRVToolsVInfoHardwareVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		upgrade *vsphere.VMHardwareUpgrade
		want    map[string]string
	}{
		{"current", "vmx-21", &vsphere.VMHardwareUpgrade{Policy: "never", Status: "none"},
			map[string]string{"HW version": "21", "HW upgrade status": "none", "HW upgrade policy": "never", "HW target": ""}},
		{"scheduled upgrade", "vmx-10", &vsphere.VMHardwareUpgrade{Policy: "onSoftPowerOff", Status: "pending", TargetVersion: "vmx-21"},
			map[string]string{"HW version": "10", "HW upgrade status": "pending", "HW upgrade policy": "onSoftPowerOff", "HW target": "21"}},
		{"captured before schema 23", "", nil,
			map[string]string{"HW version": "", "HW upgrade status": "", "HW upgrade policy": "", "HW target": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			data.VMs[0].Observation.VM.HardwareVersion = tc.version
			data.VMs[0].Observation.VM.HardwareUpgrade = tc.upgrade
			var buf bytes.Buffer
			if err := WriteRVTools(&buf, data, healthReport(data)); err != nil {
				t.Fatal(err)
			}
			f, err := excelize.OpenReader(&buf)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			rows, err := f.GetRows("vInfo")
			if err != nil || len(rows) < 2 {
				t.Fatalf("vInfo rows=%v err=%v", rows, err)
			}
			for header, want := range tc.want {
				got, found := "", false
				for i, h := range rows[0] {
					if h == header {
						found = true
						if i < len(rows[1]) {
							got = rows[1][i]
						}
					}
				}
				if !found || got != want {
					t.Errorf("%s = %q (column present: %v), want %q", header, got, found, want)
				}
			}
		})
	}
}
