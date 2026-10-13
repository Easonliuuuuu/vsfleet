package rvimport

import (
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// RVTools writes the hardware version as a bare number. The cell values are
// those of the real RVTools 4.8.1.4 export of the nested lab.
func TestVInfoHardwareVersionIsReadAsAVersionKey(t *testing.T) {
	path := writeFixtureWorkbook(t, nil)
	for _, tc := range []struct {
		name                            string
		version, status, policy, target string
		wantVersion                     string
		wantUpgrade                     *vsphere.VMHardwareUpgrade
	}{
		{"current", "21", "none", "never", "", "vmx-21", &vsphere.VMHardwareUpgrade{Policy: "never", Status: "none"}},
		{"scheduled upgrade", "10", "pending", "onSoftPowerOff", "21", "vmx-10", &vsphere.VMHardwareUpgrade{Policy: "onSoftPowerOff", Status: "pending", TargetVersion: "vmx-21"}},
		{"not reported", "", "", "", "", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openFixture(t, path)
			row := vInfoRow(t, f, "web-01")
			setColumnValue(t, f, sheetVInfo, "HW version", row, tc.version)
			setColumnValue(t, f, sheetVInfo, "HW upgrade status", row, tc.status)
			setColumnValue(t, f, sheetVInfo, "HW upgrade policy", row, tc.policy)
			setColumnValue(t, f, sheetVInfo, "HW target", row, tc.target)
			result, err := Parse(f, Options{})
			if err != nil {
				t.Fatal(err)
			}
			vm := findVM(result, "alpha", "web-01")
			if vm.HardwareVersion != tc.wantVersion {
				t.Errorf("hardware version = %q, want %q", vm.HardwareVersion, tc.wantVersion)
			}
			if (vm.HardwareUpgrade == nil) != (tc.wantUpgrade == nil) || vm.HardwareUpgrade != nil && *vm.HardwareUpgrade != *tc.wantUpgrade {
				t.Errorf("hardware upgrade = %+v, want %+v", vm.HardwareUpgrade, tc.wantUpgrade)
			}
		})
	}
}

// vInfoRow returns the 1-based worksheet row of the named VM.
func vInfoRow(t *testing.T, f *excelize.File, name string) int {
	t.Helper()
	rows, err := f.GetRows(sheetVInfo)
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range rows[1:] {
		if len(row) > 0 && row[0] == name {
			return i + 2
		}
	}
	t.Fatalf("vInfo has no row for %q", name)
	return 0
}
