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
