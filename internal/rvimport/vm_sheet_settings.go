package rvimport

import (
	"strconv"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The vCPU, vMemory and vTools settings and guest states join the ones vInfo
// recorded. A row without any of them, as in a workbook vsfleet exported
// before inventory schema 32, leaves Settings and State as vInfo left them.

func applyCPUSettingsCells(t sheetTable, row []string, vm *vsphere.VM) {
	level, shares := t.cell(row, "Level"), parseOptInt32(t.cell(row, "Shares"))
	add, remove, vnuma := parseOptBool(t.cell(row, "Hot Add")), parseOptBool(t.cell(row, "Hot Remove")), parseOptBool(t.cell(row, "Numa Hotadd Exposed"))
	if level == "" && shares == nil && add == nil && remove == nil && vnuma == nil {
		return
	}
	s := settingsFor(vm)
	s.CPUSharesLevel, s.CPUShares, s.CPUHotAdd, s.CPUHotRemove, s.VNUMAOnCPUHotAdd = level, shares, add, remove, vnuma
}

func applyMemorySettingsCells(t sheetTable, row []string, vm *vsphere.VM) {
	level, shares, add := t.cell(row, "Level"), parseOptInt32(t.cell(row, "Shares")), parseOptBool(t.cell(row, "Hot Add"))
	if level == "" && shares == nil && add == nil {
		return
	}
	s := settingsFor(vm)
	s.MemorySharesLevel, s.MemoryShares, s.MemoryHotAdd = level, shares, add
}

// applyToolsSettingsCells reads vTools' Tools settings and guest states, and
// VM Version when vInfo carried no hardware version.
func applyToolsSettingsCells(t sheetTable, row []string, vm *vsphere.VM) {
	if v := parseOptInt32(t.cell(row, "VM Version")); v != nil && vm.HardwareVersion == "" {
		vm.HardwareVersion = "vmx-" + strconv.Itoa(int(*v))
	}
	policy, sync := t.cell(row, "Upgrade Policy"), parseOptBool(t.cell(row, "Sync time"))
	if policy != "" || sync != nil {
		s := settingsFor(vm)
		s.ToolsUpgradePolicy, s.ToolsSyncTime = policy, sync
	}
	required := t.cell(row, "Required Version")
	crashed, ready := parseOptBool(t.cell(row, "Kernel Crash state")), parseOptBool(t.cell(row, "Operation Ready"))
	stateChange, interactive := parseOptBool(t.cell(row, "State change support")), parseOptBool(t.cell(row, "Interactive Guest"))
	if required == "" && crashed == nil && ready == nil && stateChange == nil && interactive == nil {
		return
	}
	st := stateFor(vm)
	st.ToolsRequiredVersion, st.GuestKernelCrashed, st.GuestOperationsReady = required, crashed, ready
	st.GuestStateChangeSupported, st.InteractiveGuestOperationsReady = stateChange, interactive
}

func settingsFor(vm *vsphere.VM) *vsphere.VMSettings {
	if vm.Settings == nil {
		vm.Settings = &vsphere.VMSettings{}
	}
	return vm.Settings
}

func stateFor(vm *vsphere.VM) *vsphere.VMState {
	if vm.State == nil {
		vm.State = &vsphere.VMState{}
	}
	return vm.State
}
