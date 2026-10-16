package rvimport

import (
	"reflect"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
)

// vDisk is read under RVTools 4.8.1.4's header names, which vsfleet now
// exports, and under the names vsfleet exported before inventory schema 24.
// The values are those of a disk row in the real RVTools export of the
// nested lab.
func TestVDiskControllerColumnsReadUnderEitherSpelling(t *testing.T) {
	unit, controllerUnit := int32(0), int32(7)
	path := writeFixtureWorkbook(t, func(data *assessment.ExportData) {
		disk := &data.VMs[0].Observation.VM.Disks[0]
		disk.Controller, disk.ControllerLabel = "LSI Logic", "SCSI controller 0"
		disk.ControllerUnitNumber, disk.UnitNumber = &controllerUnit, &unit
		disk.SharedBus, disk.RawCompatibilityMode = "physicalSharing", "physicalMode"
	})
	for _, tc := range []struct {
		name               string
		renames            map[string]string
		wantControllerUnit *int32
	}{
		{"RVTools", nil, &controllerUnit},
		{"vsfleet before schema 24", map[string]string{
			"Label": "SCSI label", "Unit #": "Unit number", "Shared Bus": "SharedBus", "Raw Comp. Mode": "Raw Compatibility Mode",
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openFixture(t, path)
			for from, to := range tc.renames {
				setHeader(t, f, sheetVDisk, []string{from}, to)
			}
			if tc.wantControllerUnit == nil {
				setHeader(t, f, sheetVDisk, []string{"SCSI Unit #"}, "Removed")
			}
			result, err := Parse(f, Options{})
			if err != nil {
				t.Fatal(err)
			}
			vm := findVM(result, "alpha", "web-01")
			if vm == nil || len(vm.Disks) != 1 {
				t.Fatalf("vDisk row was not imported: %+v", vm)
			}
			got := vm.Disks[0]
			if got.ControllerLabel != "SCSI controller 0" || got.SharedBus != "physicalSharing" || got.RawCompatibilityMode != "physicalMode" {
				t.Errorf("controller columns = label %q, shared bus %q, raw mode %q", got.ControllerLabel, got.SharedBus, got.RawCompatibilityMode)
			}
			if !reflect.DeepEqual(got.UnitNumber, &unit) || !reflect.DeepEqual(got.ControllerUnitNumber, tc.wantControllerUnit) {
				t.Errorf("unit numbers = disk %v, controller %v", got.UnitNumber, got.ControllerUnitNumber)
			}
		})
	}
}
