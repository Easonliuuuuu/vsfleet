package rvimport

import "github.com/easonliuuuuu/vsfleet/internal/vsphere"

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

func mibBytes(cell string) *int64 {
	f, ok := parseNumber(cell)
	if !ok {
		return nil
	}
	v := int64(f * mib)
	return &v
}
