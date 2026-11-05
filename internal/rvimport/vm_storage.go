package rvimport

import (
	"math"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// applyVMStorageCells reads vInfo's Provisioned MiB, Unshared MiB,
// Consolidation Needed and file location columns. An empty cell or a missing
// column leaves the field unset, so an older workbook imports them as "not
// read" rather than as zero or "no files".
func applyVMStorageCells(t sheetTable, row []string, vm *vsphere.VM) {
	vm.ProvisionedBytes = mibBytes(t.cell(row, "Provisioned MiB"))
	vm.UnsharedBytes = mibBytes(t.cell(row, "Unshared MiB"))
	vm.ConsolidationNeeded = parseOptBool(t.cell(row, "Consolidation Needed"))
	files := vsphere.VMFiles{
		VMPath:            t.cell(row, "Path"),
		LogDirectory:      t.cell(row, "Log directory"),
		SnapshotDirectory: t.cell(row, "Snapshot directory"),
		SuspendDirectory:  t.cell(row, "Suspend directory"),
	}
	if files != (vsphere.VMFiles{}) {
		vm.Files = &files
	}
}

// mibBytes converts a MiB cell to bytes, nil when it holds no number.
// RVTools writes fractional MiB, such as a snapshot's 0.0092 MiB, so the
// bytes are rounded rather than truncated.
func mibBytes(cell string) *int64 {
	f, ok := parseNumber(cell)
	if !ok {
		return nil
	}
	v := int64(math.Round(f * mib))
	return &v
}
