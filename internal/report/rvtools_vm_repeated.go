package report

import "github.com/easonliuuuuu/vsfleet/internal/vsphere"

// RVTools repeats the Tools-reported guest OS on most VM sheets, and spells
// it with a lower-case t on vUSB only.
const (
	osToolsHeader    = "OS according to the VMware Tools"
	usbOSToolsHeader = "OS according to the VMware tools"
)

// vmTailHeadersWith returns the identity, location and provenance tail of a
// per-VM sheet, with the Tools-reported OS column under osTools.
func vmTailHeadersWith(osTools string) []string {
	return []string{"Annotation", "Datacenter", "Cluster", "Host", "Folder", "OS according to the configuration file", osTools,
		"VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID", "vsfleet Context"}
}

// srmPlaceholderCell is empty for a VM captured before inventory schema 31
// or read without its configuration.
func srmPlaceholderCell(vm vsphere.VM) any {
	if vm.Settings == nil {
		return nil
	}
	return optionalBool(vm.Settings.SRMPlaceholder)
}

// guestOSReportedCell is empty when VMware Tools reports no guest OS, or for
// a VM captured before inventory schema 31.
func guestOSReportedCell(vm vsphere.VM) any {
	if vm.State == nil {
		return nil
	}
	return optionalString(vm.State.GuestOSReported)
}
