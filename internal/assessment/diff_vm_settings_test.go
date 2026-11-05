package assessment

import (
	"reflect"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// Settings fields added in inventory schema 29 change only between two runs
// that recorded them; state needs --include-runtime, like power state.
func TestCompareVMsReportsSchema29FieldsOnlyWhenBothRunsRecordedThem(t *testing.T) {
	i64 := func(v int64) *int64 { return &v }
	yes, no := true, false
	for _, tc := range []struct {
		name          string
		before, after func(*vsphere.VM)
		runtime       bool
		want          []FieldChange
	}{
		{"boot delay and CBT changed",
			func(vm *vsphere.VM) { vm.Settings = &vsphere.VMSettings{BootDelay: i64(0), ChangeTracking: &no} },
			func(vm *vsphere.VM) { vm.Settings = &vsphere.VMSettings{BootDelay: i64(5000), ChangeTracking: &yes} }, false,
			[]FieldChange{{Field: "settings.boot_delay", Before: "0", After: "5000"}, {Field: "settings.change_tracking", Before: "false", After: "true"}}},
		{"vCenter upgraded to report EnableUUID",
			func(vm *vsphere.VM) { vm.Settings = &vsphere.VMSettings{BootDelay: i64(0)} },
			func(vm *vsphere.VM) { vm.Settings = &vsphere.VMSettings{BootDelay: i64(0), EnableUUID: &yes} }, false, nil},
		{"base captured before schema 29", func(*vsphere.VM) {},
			func(vm *vsphere.VM) {
				vm.Settings, vm.State = &vsphere.VMSettings{BootDelay: i64(0)}, &vsphere.VMState{ConfigStatus: "green"}
			}, true, nil},
		{"config status changed",
			func(vm *vsphere.VM) { vm.State = &vsphere.VMState{ConfigStatus: "green"} },
			func(vm *vsphere.VM) { vm.State = &vsphere.VMState{ConfigStatus: "yellow"} }, true,
			[]FieldChange{{Field: "state.config_status", Before: `"green"`, After: `"yellow"`}}},
		{"config status changed without runtime fields",
			func(vm *vsphere.VM) { vm.State = &vsphere.VMState{ConfigStatus: "green"} },
			func(vm *vsphere.VM) { vm.State = &vsphere.VMState{ConfigStatus: "yellow"} }, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := stored("app", "vc-a", "vm-1", "instance-1")
			target := base
			tc.before(&base.observation.VM)
			tc.after(&target.observation.VM)
			changes, _ := compareVMs([]storedVM{base}, []storedVM{target}, tc.runtime)
			var got []FieldChange
			for _, c := range changes {
				got = append(got, c.Fields...)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("fields = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Schema 31 adds whether a VM is an SRM placeholder, a setting, and the guest
// OS VMware Tools reports, which is state. A run from before schema 31 has
// settings without the placeholder flag, which is not a change.
func TestCompareVMsReportsSRMPlaceholderAndToolsOS(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name          string
		before, after func(*vsphere.VM)
		runtime       bool
		want          []FieldChange
	}{
		{"became an SRM placeholder",
			func(vm *vsphere.VM) { vm.Settings = &vsphere.VMSettings{SRMPlaceholder: &no} },
			func(vm *vsphere.VM) { vm.Settings = &vsphere.VMSettings{SRMPlaceholder: &yes} }, false,
			[]FieldChange{{Field: "settings.srm_placeholder", Before: "false", After: "true"}}},
		{"base captured before schema 31",
			func(vm *vsphere.VM) { vm.Settings = &vsphere.VMSettings{} },
			func(vm *vsphere.VM) { vm.Settings = &vsphere.VMSettings{SRMPlaceholder: &no} }, false, nil},
		{"guest upgraded",
			func(vm *vsphere.VM) { vm.State = &vsphere.VMState{GuestOSReported: "Ubuntu Linux (64-bit)"} },
			func(vm *vsphere.VM) { vm.State = &vsphere.VMState{GuestOSReported: "Rocky Linux (64-bit)"} }, true,
			[]FieldChange{{Field: "state.guest_os_reported", Before: `"Ubuntu Linux (64-bit)"`, After: `"Rocky Linux (64-bit)"`}}},
		{"guest upgraded without runtime fields",
			func(vm *vsphere.VM) { vm.State = &vsphere.VMState{GuestOSReported: "Ubuntu Linux (64-bit)"} },
			func(vm *vsphere.VM) { vm.State = &vsphere.VMState{GuestOSReported: "Rocky Linux (64-bit)"} }, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := stored("app", "vc-a", "vm-1", "instance-1")
			target := base
			tc.before(&base.observation.VM)
			tc.after(&target.observation.VM)
			changes, _ := compareVMs([]storedVM{base}, []storedVM{target}, tc.runtime)
			var got []FieldChange
			for _, c := range changes {
				got = append(got, c.Fields...)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("fields = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Hot-plug, shares and Tools settings are configuration, compared on every
// diff. The host's bundled Tools version and the guest operation states are
// compared only with runtime fields: a host upgrade changes the first, and
// the guest itself the others.
func TestCompareVMsReportsVMSheetSettings(t *testing.T) {
	yes, no := true, false
	low, normal := int32(500), int32(1000)
	for _, tc := range []struct {
		name          string
		before, after func(*vsphere.VM)
		runtime       bool
		want          []FieldChange
	}{
		{"CPU hot add enabled and shares lowered",
			func(vm *vsphere.VM) {
				vm.Settings = &vsphere.VMSettings{CPUHotAdd: &no, CPUShares: &normal, CPUSharesLevel: "normal"}
			},
			func(vm *vsphere.VM) {
				vm.Settings = &vsphere.VMSettings{CPUHotAdd: &yes, CPUShares: &low, CPUSharesLevel: "low"}
			}, false,
			[]FieldChange{{Field: "settings.cpu_hot_add", Before: "false", After: "true"}, {Field: "settings.cpu_shares", Before: "1000", After: "500"},
				{Field: "settings.cpu_shares_level", Before: `"normal"`, After: `"low"`}}},
		{"base captured before schema 32",
			func(vm *vsphere.VM) { vm.Settings = &vsphere.VMSettings{SRMPlaceholder: &no} },
			func(vm *vsphere.VM) {
				vm.Settings = &vsphere.VMSettings{SRMPlaceholder: &no, MemoryHotAdd: &yes, ToolsUpgradePolicy: "manual"}
			}, false, nil},
		{"host upgraded",
			func(vm *vsphere.VM) { vm.State = &vsphere.VMState{ToolsRequiredVersion: "12448"} },
			func(vm *vsphere.VM) { vm.State = &vsphere.VMState{ToolsRequiredVersion: "12450"} }, true,
			[]FieldChange{{Field: "state.tools_required_version", Before: `"12448"`, After: `"12450"`}}},
		{"host upgraded without runtime fields",
			func(vm *vsphere.VM) {
				vm.State = &vsphere.VMState{ToolsRequiredVersion: "12448", GuestOperationsReady: &no}
			},
			func(vm *vsphere.VM) {
				vm.State = &vsphere.VMState{ToolsRequiredVersion: "12450", GuestOperationsReady: &yes}
			}, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := stored("app", "vc-a", "vm-1", "instance-1")
			target := base
			tc.before(&base.observation.VM)
			tc.after(&target.observation.VM)
			changes, _ := compareVMs([]storedVM{base}, []storedVM{target}, tc.runtime)
			var got []FieldChange
			for _, c := range changes {
				got = append(got, c.Fields...)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("fields = %+v, want %+v", got, tc.want)
			}
		})
	}
}
