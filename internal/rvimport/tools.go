package rvimport

import "github.com/easonliuuuuu/vsfleet/internal/vsphere"

// Older vsfleet exports wrote running status into Tools. Preserve those
// values without interpreting RVTools installation status as running status.
func applyToolsStatus(status, running string, vm *vsphere.VM) {
	vm.ToolsStatus = ""
	vm.ToolsState = running
	switch status {
	case "guestToolsRunning", "guestToolsNotRunning", "guestToolsExecutingScripts":
		if vm.ToolsState == "" {
			vm.ToolsState = status
		}
	default:
		vm.ToolsStatus = status
	}
}

// deriveToolsDetail fills the detailed Tools fields a workbook left empty
// from the two installation values that imply them. Real RVTools 4.8 writes
// Tools without Tools Running Status or Tools Version Status. toolsOk and
// toolsOld imply nothing here: the legacy status is deprecated and they are
// left as missing evidence rather than read as running or current.
func deriveToolsDetail(vm *vsphere.VM) {
	switch vm.ToolsStatus {
	case "toolsNotInstalled":
		if vm.ToolsVersionStatus == "" {
			vm.ToolsVersionStatus = "guestToolsNotInstalled"
		}
		if vm.ToolsState == "" {
			vm.ToolsState = "guestToolsNotRunning"
		}
	case "toolsNotRunning":
		if vm.ToolsState == "" {
			vm.ToolsState = "guestToolsNotRunning"
		}
	}
}
