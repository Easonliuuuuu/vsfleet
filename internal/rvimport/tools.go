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
