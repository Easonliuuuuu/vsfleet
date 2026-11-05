package vsphere

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// A vCenter asked for a property it does not have fails the whole VM
// retrieval, so each newer property is requested only from a vCenter that
// has it. A listing reads none of them.
func TestPropsForAPIRequestsOnlyPropertiesTheVCenterHas(t *testing.T) {
	for _, tc := range []struct {
		name, api string
		detail    Detail
		want, not []string
	}{
		{"vCenter 7.0", "7.0.0.0", DetailFull, []string{"config.createDate", "runtime.bootTime", "config.cpuHotAddEnabled", "layoutEx.snapshot"},
			[]string{"guest.customizationInfo", `config.extraConfig["disk.EnableUUID"]`, `config.extraConfig["vmware.tools.requiredversion"]`,
				"config.rebootPowerOff", "config.numaInfo", "guest.guestDetailedData"}},
		{"vCenter 7.0 Update 2", "7.0.2.0", DetailFull, []string{"guest.customizationInfo"}, []string{"config.rebootPowerOff"}},
		{"vCenter 8.0", "8.0.0.0", DetailFull, []string{`config.extraConfig["vmware.tools.requiredversion"]`}, []string{"config.numaInfo"}},
		{"vCenter 8.0 Update 1", "8.0.1.0", DetailFull, []string{`config.extraConfig["disk.EnableUUID"]`, "config.rebootPowerOff", "config.numaInfo", "config.fixedPassthruHotPlugEnabled"},
			[]string{"guest.guestDetailedData"}},
		{"vCenter 8.0 Update 3", "8.0.3.0", DetailFull, []string{"guest.guestDetailedData"}, nil},
		{"unparseable version", "", DetailFull, []string{"config.createDate"}, []string{"guest.customizationInfo"}},
		{"listing", "8.0.3.0", DetailSummary, nil, []string{"config.createDate", "guest.guestDetailedData", "config.cpuHotAddEnabled", "layoutEx.file"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			props := propsForAPI(tc.detail, tc.api)
			for _, p := range tc.want {
				if !slices.Contains(props, p) {
					t.Errorf("missing %s", p)
				}
			}
			for _, p := range tc.not {
				if slices.Contains(props, p) {
					t.Errorf("requested %s from API %q", p, tc.api)
				}
			}
		})
	}
}

// The full-capture values are those vCenter 8.0.3 returns for the nested
// lab's vcsa, plus the settings the lab leaves at their defaults. vcsa's
// numaInfo leaves vnumaOnCpuHotaddExposed out, which means it is off.
func TestVMSettingsAndStateConvertVCenterValues(t *testing.T) {
	created := time.Date(2026, 9, 27, 15, 45, 42, 0, time.UTC)
	booted := time.Date(2026, 11, 2, 12, 25, 6, 0, time.UTC)
	no, yes := false, true
	cores := int32(2)
	m := &mo.VirtualMachine{
		ManagedEntity: mo.ManagedEntity{ConfigStatus: types.ManagedEntityStatusGreen},
		Runtime:       types.VirtualMachineRuntimeInfo{BootTime: &booted, MinRequiredEVCModeKey: "intel-cascadelake"},
		Guest: &types.GuestInfo{GuestDetailedData: "distroName='VMware Photon OS'", GuestFullName: "VMware Photon OS (64-bit)",
			CustomizationInfo:  &types.GuestInfoCustomizationInfo{CustomizationStatus: "TOOLSDEPLOYPKG_IDLE"},
			GuestKernelCrashed: &no, GuestOperationsReady: &yes, InteractiveGuestOperationsReady: &no, GuestStateChangeSupported: &yes},
		Config: &types.VirtualMachineConfigInfo{
			CreateDate: &created, ChangeVersion: "2026-11-02T12:25:03.553253Z", ChangeTrackingEnabled: &no,
			LatencySensitivity: &types.LatencySensitivity{Level: types.LatencySensitivitySensitivityLevelNormal},
			RebootPowerOff:     &no, VmOpNotificationTimeout: -1, FixedPassthruHotPlugEnabled: &no,
			BootOptions: &types.VirtualMachineBootOptions{EnterBIOSSetup: &no, BootRetryEnabled: &no, BootRetryDelay: 10000},
			ExtraConfig: []types.BaseOptionValue{&types.OptionValue{Key: "disk.EnableUUID", Value: "TRUE"},
				&types.OptionValue{Key: "vmware.tools.requiredversion", Value: "12448"}},
			Hardware:         types.VirtualHardware{Device: []types.BaseVirtualDevice{&types.VirtualMachineVideoCard{NumDisplays: 1, VideoRamSizeInKB: 4096}}},
			CpuHotAddEnabled: &yes, CpuHotRemoveEnabled: &yes, MemoryHotAddEnabled: &yes,
			NumaInfo:         &types.VirtualMachineVirtualNumaInfo{CoresPerNumaNode: &cores, AutoCoresPerNumaNode: &yes},
			CpuAllocation:    &types.ResourceAllocationInfo{Shares: &types.SharesInfo{Shares: 2000, Level: types.SharesLevelNormal}},
			MemoryAllocation: &types.ResourceAllocationInfo{Shares: &types.SharesInfo{Shares: 143360, Level: types.SharesLevelNormal}},
			Tools:            &types.ToolsConfigInfo{ToolsUpgradePolicy: "manual", SyncTimeWithHost: &yes},
		},
	}
	zero, retry, monitors, ram, interval := int64(0), int64(10000), int32(1), int64(4096), int64(0)
	cpuShares, memShares := int32(2000), int32(143360)
	wantSettings := &VMSettings{CreateDate: &created, EnableUUID: &yes, ChangeTracking: &no, LatencySensitivity: "normal", Monitors: &monitors,
		VideoRAMKiB: &ram, FixedPassthruHotPlug: &no, InstallBootRequired: &no, EnterBIOSSetup: &no, BootDelay: &zero, BootRetryEnabled: &no, BootRetryDelay: &retry,
		SRMPlaceholder: &no, CPUHotAdd: &yes, CPUHotRemove: &yes, VNUMAOnCPUHotAdd: &no, MemoryHotAdd: &yes, CPUShares: &cpuShares, CPUSharesLevel: "normal",
		MemoryShares: &memShares, MemorySharesLevel: "normal", ToolsUpgradePolicy: "manual", ToolsSyncTime: &yes}
	if got := vmSettings(m, "8.0.3.0"); !reflect.DeepEqual(got, wantSettings) {
		t.Errorf("settings = %+v\nwant       %+v", got, wantSettings)
	}
	wantState := &VMState{ChangeVersion: "2026-11-02T12:25:03.553253Z", ConfigStatus: "green", BootTime: &booted, RebootPowerOff: &no,
		SuspendInterval: &interval, MinRequiredEVCModeKey: "intel-cascadelake", CustomizationStatus: "TOOLSDEPLOYPKG_IDLE", GuestDetailedData: "distroName='VMware Photon OS'",
		GuestOSReported: "VMware Photon OS (64-bit)", GuestKernelCrashed: &no, GuestOperationsReady: &yes, InteractiveGuestOperationsReady: &no,
		GuestStateChangeSupported: &yes, ToolsRequiredVersion: "12448"}
	if got := vmState(m); !reflect.DeepEqual(got, wantState) {
		t.Errorf("state = %+v\nwant    %+v", got, wantState)
	}

	// vCenter 7.0 was not asked for vmOpNotificationTimeout, so its zero
	// value is not a timeout of zero.
	m.Config.VmOpNotificationTimeout = 0
	if got := vmSettings(m, "7.0.3.0").OpNotificationTimeout; got != nil {
		t.Errorf("op notification timeout from vCenter 7.0 = %d, want unset", *got)
	}
	if got := vmSettings(m, "8.0.3.0").OpNotificationTimeout; got == nil || *got != 0 {
		t.Errorf("op notification timeout 0 from vCenter 8.0 = %v, want 0", got)
	}
	// Nor was it asked for numaInfo, so vNUMA exposure is unknown, not off.
	if got := vmSettings(m, "8.0.0.0").VNUMAOnCPUHotAdd; got != nil {
		t.Errorf("vNUMA on CPU hot add from vCenter 8.0 = %v, want unset", *got)
	}
}

// The layout is vm-snapshot's on the nested lab: snap-1 froze the base disk
// (a 475-byte descriptor and an empty thin extent), snap-2 the first delta.
// RVTools reports 0.0092 MiB (vmsn) and 0.000453 and 0.0042 MiB (total).
func TestSnapshotFilesComeFromTheFileLayout(t *testing.T) {
	file := func(key int32, name string, size int64) types.VirtualMachineFileLayoutExFileInfo {
		return types.VirtualMachineFileLayoutExFileInfo{Key: key, Name: name, Size: size}
	}
	chain := func(keys ...int32) types.VirtualMachineFileLayoutExDiskUnit {
		return types.VirtualMachineFileLayoutExDiskUnit{FileKey: keys}
	}
	snapshot := func(id string, data int32, links ...types.VirtualMachineFileLayoutExDiskUnit) types.VirtualMachineFileLayoutExSnapshotLayout {
		return types.VirtualMachineFileLayoutExSnapshotLayout{Key: types.ManagedObjectReference{Type: "VirtualMachineSnapshot", Value: id}, DataKey: data,
			Disk: []types.VirtualMachineFileLayoutExDiskLayout{{Key: 2000, Chain: links}}}
	}
	layout := &types.VirtualMachineFileLayoutEx{
		File: []types.VirtualMachineFileLayoutExFileInfo{
			file(2, "[nfs-shared] vm-snapshot/vm-snapshot.vmdk", 475), file(3, "[nfs-shared] vm-snapshot/vm-snapshot-flat.vmdk", 0),
			file(4, "[nfs-shared] vm-snapshot/vm-snapshot-000001.vmdk", 302), file(5, "[nfs-shared] vm-snapshot/vm-snapshot-000001-delta.vmdk", 4096),
			file(8, "[nfs-shared] vm-snapshot/vm-snapshot-Snapshot1.vmsn", 9651), file(9, "[nfs-shared] vm-snapshot/vm-snapshot-Snapshot2.vmsn", 9658),
		},
		Snapshot: []types.VirtualMachineFileLayoutExSnapshotLayout{
			snapshot("snapshot-1017", 8, chain(2, 3)),
			snapshot("snapshot-1018", 9, chain(2, 3), chain(4, 5)),
		},
	}
	snaps := []VMSnapshot{{ID: "snapshot-1017"}, {ID: "snapshot-1018"}, {ID: "snapshot-gone"}}
	applySnapshotFiles(snaps, layout)
	for i, want := range []struct {
		file        string
		data, total int64
	}{
		{"[nfs-shared] vm-snapshot/vm-snapshot-Snapshot1.vmsn", 9651, 475},
		{"[nfs-shared] vm-snapshot/vm-snapshot-Snapshot2.vmsn", 9658, 4398},
	} {
		got := snaps[i]
		if got.Filename != want.file || got.DataBytes == nil || *got.DataBytes != want.data || got.TotalBytes == nil || *got.TotalBytes != want.total {
			t.Errorf("%s = %q, %v, %v, want %q, %d, %d", got.ID, got.Filename, got.DataBytes, got.TotalBytes, want.file, want.data, want.total)
		}
	}
	if gone := snaps[2]; gone.Filename != "" || gone.DataBytes != nil || gone.TotalBytes != nil {
		t.Errorf("snapshot missing from the layout = %+v, want no files", gone)
	}
}

// Site Recovery Manager marks the placeholder it keeps at the recovery site
// with its own extension key and type. Another extension's VM, such as an
// ESX Agent Manager cluster agent, is not a placeholder.
func TestVMSettingsMarksOnlySRMPlaceholders(t *testing.T) {
	for _, tc := range []struct {
		name      string
		managedBy *types.ManagedByInfo
		want      bool
	}{
		{"SRM placeholder", &types.ManagedByInfo{ExtensionKey: "com.vmware.vcDr", Type: "placeholderVm"}, true},
		{"SRM-managed, not a placeholder", &types.ManagedByInfo{ExtensionKey: "com.vmware.vcDr", Type: "testVm"}, false},
		{"cluster agent", &types.ManagedByInfo{ExtensionKey: "com.vmware.vim.eam", Type: "cluster-agent"}, false},
		{"unmanaged", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &mo.VirtualMachine{Config: &types.VirtualMachineConfigInfo{ManagedBy: tc.managedBy}}
			if got := vmSettings(m, "8.0.3.0").SRMPlaceholder; got == nil || *got != tc.want {
				t.Errorf("SRM placeholder = %v, want %v", got, tc.want)
			}
		})
	}
}
