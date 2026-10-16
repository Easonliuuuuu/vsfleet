package rvimport

import "github.com/easonliuuuuu/vsfleet/internal/vsphere"

// applyCPUAllocationRow reads vCPU's topology and allocation columns. An
// empty cell leaves the field unset, so a workbook without these columns
// imports as "not read" rather than as zero sockets or no reservation.
func applyCPUAllocationRow(t sheetTable, row []string, vm *vsphere.VM) {
	if sockets := int32(parseInt(t.cell(row, "Sockets"))); sockets > 0 {
		vm.CPUSockets = sockets
	}
	if cores := int32(parseInt(t.cell(row, "Cores p/s"))); cores > 0 {
		vm.CoresPerSocket = cores
	}
	vm.CPUAllocation = allocationFromRow(t, row)
}

// applyMemoryAllocationRow reads vMemory's reservation columns.
func applyMemoryAllocationRow(t sheetTable, row []string, vm *vsphere.VM) {
	vm.MemoryReservationLockedToMax = parseOptBool(t.cell(row, "Memory Reservation Locked To Max"))
	vm.MemoryAllocation = allocationFromRow(t, row)
}

func allocationFromRow(t sheetTable, row []string) *vsphere.VMResourceAllocation {
	a := vsphere.VMResourceAllocation{Reservation: parseOptInt64(t.cell(row, "Reservation")), Limit: parseOptInt64(t.cell(row, "Limit"))}
	if a.Reservation == nil && a.Limit == nil {
		return nil
	}
	return &a
}
