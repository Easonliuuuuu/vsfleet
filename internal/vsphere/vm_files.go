package vsphere

import (
	"strings"

	"github.com/vmware/govmomi/vim25/types"
)

// vmFiles copies where a VM keeps its files. It returns nil when vSphere
// reported none of them, as a listing that did not ask for config.files gets.
func vmFiles(in types.VirtualMachineFileInfo) *VMFiles {
	files := VMFiles{
		VMPath:            strings.TrimSpace(in.VmPathName),
		LogDirectory:      strings.TrimSpace(in.LogDirectory),
		SnapshotDirectory: strings.TrimSpace(in.SnapshotDirectory),
		SuspendDirectory:  strings.TrimSpace(in.SuspendDirectory),
	}
	if files == (VMFiles{}) {
		return nil
	}
	return &files
}
