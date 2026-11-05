package vsphere

import (
	"strings"

	"github.com/vmware/govmomi/vim25/types"
)

// toolsRequiredVersionKey is the advanced setting in which the host records
// the VMware Tools version it bundles, the one RVTools reports as Required
// Version.
const toolsRequiredVersionKey = "vmware.tools.requiredversion"

// vmSheetSettingsProps are the vCPU, vMemory, vTools and vSnapshot properties
// every supported vCenter has. config.cpuAllocation and
// config.memoryAllocation, which carry the shares, are read with the device
// properties.
var vmSheetSettingsProps = []string{
	"config.cpuHotAddEnabled",
	"config.cpuHotRemoveEnabled",
	"config.memoryHotAddEnabled",
	"config.tools.toolsUpgradePolicy",
	"config.tools.syncTimeWithHost",
	"guest.guestKernelCrashed",
	"guest.guestOperationsReady",
	"guest.interactiveGuestOperationsReady",
	"guest.guestStateChangeSupported",
	"layoutEx.file",
	"layoutEx.snapshot",
}

// applySheetSettings copies the hot-plug, shares and Tools settings of cfg,
// read from a vCenter that speaks apiVersion.
func applySheetSettings(s *VMSettings, cfg *types.VirtualMachineConfigInfo, apiVersion string) {
	s.CPUHotAdd, s.CPUHotRemove, s.MemoryHotAdd = clonePtr(cfg.CpuHotAddEnabled), clonePtr(cfg.CpuHotRemoveEnabled), clonePtr(cfg.MemoryHotAddEnabled)
	// vSphere leaves vnumaOnCpuHotaddExposed out when it is off, its default,
	// so a vCenter that returned numaInfo at all reported false.
	if n := cfg.NumaInfo; n != nil && apiAtLeast(apiVersion, "8.0.0.1") {
		exposed := n.VnumaOnCpuHotaddExposed != nil && *n.VnumaOnCpuHotaddExposed
		s.VNUMAOnCPUHotAdd = &exposed
	}
	s.CPUShares, s.CPUSharesLevel = allocationShares(cfg.CpuAllocation)
	s.MemoryShares, s.MemorySharesLevel = allocationShares(cfg.MemoryAllocation)
	if t := cfg.Tools; t != nil {
		s.ToolsUpgradePolicy, s.ToolsSyncTime = strings.TrimSpace(t.ToolsUpgradePolicy), clonePtr(t.SyncTimeWithHost)
	}
}

// extraConfigString returns the string value of one advanced setting, empty
// when the VM does not set it or it was not read.
func extraConfigString(options []types.BaseOptionValue, key string) string {
	for _, o := range options {
		if v := o.GetOptionValue(); v != nil && strings.EqualFold(v.Key, key) {
			if s, ok := v.Value.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func allocationShares(a *types.ResourceAllocationInfo) (*int32, string) {
	if a == nil || a.Shares == nil {
		return nil, ""
	}
	return cloneInt32(a.Shares.Shares), string(a.Shares.Level)
}

// applySheetState copies what VMware Tools currently reports about the guest.
func applySheetState(s *VMState, g *types.GuestInfo) {
	s.GuestKernelCrashed, s.GuestOperationsReady = clonePtr(g.GuestKernelCrashed), clonePtr(g.GuestOperationsReady)
	s.InteractiveGuestOperationsReady, s.GuestStateChangeSupported = clonePtr(g.InteractiveGuestOperationsReady), clonePtr(g.GuestStateChangeSupported)
}

// applySnapshotFiles records each snapshot's state file and sizes from the
// VM's file layout. A snapshot the layout does not name keeps nil sizes.
func applySnapshotFiles(snapshots []VMSnapshot, layout *types.VirtualMachineFileLayoutEx) {
	if layout == nil || len(snapshots) == 0 {
		return
	}
	files := make(map[int32]types.VirtualMachineFileLayoutExFileInfo, len(layout.File))
	for _, f := range layout.File {
		files[f.Key] = f
	}
	byID := make(map[string]types.VirtualMachineFileLayoutExSnapshotLayout, len(layout.Snapshot))
	for _, l := range layout.Snapshot {
		byID[l.Key.Value] = l
	}
	for i := range snapshots {
		l, ok := byID[snapshots[i].ID]
		if !ok {
			continue
		}
		if f, ok := files[l.DataKey]; ok {
			size := f.Size
			snapshots[i].Filename, snapshots[i].DataBytes = f.Name, &size
		}
		// RVTools' total is the newest link of each disk's chain when the
		// snapshot was taken: the files the snapshot froze.
		var total int64
		for _, d := range l.Disk {
			if len(d.Chain) == 0 {
				continue
			}
			for _, key := range d.Chain[len(d.Chain)-1].FileKey {
				total += files[key].Size
			}
		}
		snapshots[i].TotalBytes = &total
	}
}
