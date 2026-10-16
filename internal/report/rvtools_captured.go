package report

import (
	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// networkSlotCount is how many Network #N columns RVTools writes on vInfo. A
// VM with more NICs lists the rest only on vNetwork, as RVTools does.
const networkSlotCount = 8

// totalDiskCapacityMiB is vInfo's Total disk capacity MiB: the provisioned
// size of every virtual disk. It is empty wherever the Disks count is, so a
// VM whose disks were not read is never reported as having no storage.
func totalDiskCapacityMiB(data assessment.ExportData, vm vsphere.VM) any {
	if deviceCount(data, vm, len(vm.Disks)) == nil {
		return nil
	}
	var bytes int64
	for _, disk := range vm.Disks {
		bytes += disk.CapacityBytes
	}
	return float64(bytes) / miB
}

// networkSlots writes vInfo's Network #1 to #8: the port group of each NIC in
// device order, leaving unused slots empty.
func networkSlots(data assessment.ExportData, vm vsphere.VM) []any {
	cells := make([]any, networkSlotCount)
	if deviceCount(data, vm, len(vm.NICs)) == nil {
		return cells
	}
	for i, nic := range vm.NICs {
		if i == networkSlotCount {
			break
		}
		cells[i] = optionalString(nic.Network)
	}
	return cells
}

// allocationCells writes a VM's Reservation and Limit on vCPU or vMemory,
// both empty when vSphere returned no allocation. A limit of -1 is vSphere's
// "unlimited", written as RVTools writes it.
func allocationCells(a *vsphere.VMResourceAllocation) []any {
	if a == nil {
		return []any{nil, nil}
	}
	return []any{optionalInt64(a.Reservation), optionalInt64(a.Limit)}
}

// effectiveCapacity writes vCluster's Effective Cpu or Effective Memory. A
// zero is real only when no host can run VMs; otherwise it means the capture
// did not record the value, which is left empty rather than reported as a
// cluster with no capacity.
func effectiveCapacity(c vsphere.Cluster, value int64) any {
	if value == 0 && c.EffectiveHost > 0 {
		return nil
	}
	return value
}
