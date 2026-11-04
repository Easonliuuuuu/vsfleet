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
		{"vCenter 7.0", "7.0.0.0", DetailFull, []string{"config.createDate", "runtime.bootTime"},
			[]string{"guest.customizationInfo", `config.extraConfig["disk.EnableUUID"]`, "config.rebootPowerOff", "guest.guestDetailedData"}},
		{"vCenter 7.0 Update 2", "7.0.2.0", DetailFull, []string{"guest.customizationInfo"}, []string{"config.rebootPowerOff"}},
		{"vCenter 8.0 Update 1", "8.0.1.0", DetailFull, []string{`config.extraConfig["disk.EnableUUID"]`, "config.rebootPowerOff", "config.fixedPassthruHotPlugEnabled"},
			[]string{"guest.guestDetailedData"}},
		{"vCenter 8.0 Update 3", "8.0.3.0", DetailFull, []string{"guest.guestDetailedData"}, nil},
		{"unparseable version", "", DetailFull, []string{"config.createDate"}, []string{"guest.customizationInfo"}},
		{"listing", "8.0.3.0", DetailSummary, nil, []string{"config.createDate", "guest.guestDetailedData"}},
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
// lab's vcsa, plus the settings the lab leaves at their defaults.
func TestVMSettingsAndStateConvertVCenterValues(t *testing.T) {
	created := time.Date(2026, 9, 27, 15, 45, 42, 0, time.UTC)
	booted := time.Date(2026, 11, 2, 12, 25, 6, 0, time.UTC)
	no, yes := false, true
	m := &mo.VirtualMachine{
		ManagedEntity: mo.ManagedEntity{ConfigStatus: types.ManagedEntityStatusGreen},
		Runtime:       types.VirtualMachineRuntimeInfo{BootTime: &booted, MinRequiredEVCModeKey: "intel-cascadelake"},
		Guest: &types.GuestInfo{GuestDetailedData: "distroName='VMware Photon OS'", GuestFullName: "VMware Photon OS (64-bit)",
			CustomizationInfo: &types.GuestInfoCustomizationInfo{CustomizationStatus: "TOOLSDEPLOYPKG_IDLE"}},
		Config: &types.VirtualMachineConfigInfo{
			CreateDate: &created, ChangeVersion: "2026-11-02T12:25:03.553253Z", ChangeTrackingEnabled: &no,
			LatencySensitivity: &types.LatencySensitivity{Level: types.LatencySensitivitySensitivityLevelNormal},
			RebootPowerOff:     &no, VmOpNotificationTimeout: -1, FixedPassthruHotPlugEnabled: &no,
			BootOptions: &types.VirtualMachineBootOptions{EnterBIOSSetup: &no, BootRetryEnabled: &no, BootRetryDelay: 10000},
			ExtraConfig: []types.BaseOptionValue{&types.OptionValue{Key: "disk.EnableUUID", Value: "TRUE"}},
			Hardware:    types.VirtualHardware{Device: []types.BaseVirtualDevice{&types.VirtualMachineVideoCard{NumDisplays: 1, VideoRamSizeInKB: 4096}}},
		},
	}
	zero, retry, monitors, ram, interval := int64(0), int64(10000), int32(1), int64(4096), int64(0)
	wantSettings := &VMSettings{CreateDate: &created, EnableUUID: &yes, ChangeTracking: &no, LatencySensitivity: "normal", Monitors: &monitors,
		VideoRAMKiB: &ram, FixedPassthruHotPlug: &no, InstallBootRequired: &no, EnterBIOSSetup: &no, BootDelay: &zero, BootRetryEnabled: &no, BootRetryDelay: &retry,
		SRMPlaceholder: &no}
	if got := vmSettings(m, "8.0.3.0"); !reflect.DeepEqual(got, wantSettings) {
		t.Errorf("settings = %+v\nwant       %+v", got, wantSettings)
	}
	wantState := &VMState{ChangeVersion: "2026-11-02T12:25:03.553253Z", ConfigStatus: "green", BootTime: &booted, RebootPowerOff: &no,
		SuspendInterval: &interval, MinRequiredEVCModeKey: "intel-cascadelake", CustomizationStatus: "TOOLSDEPLOYPKG_IDLE", GuestDetailedData: "distroName='VMware Photon OS'",
		GuestOSReported: "VMware Photon OS (64-bit)"}
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
