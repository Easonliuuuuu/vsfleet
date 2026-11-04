package assessment

import (
	"reflect"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// Fields added in inventory schema 28 change only between two runs that
// recorded them; storage figures are volatile and need --include-runtime.
func TestCompareVMsReportsSchema28FieldsOnlyWhenBothRunsRecordedThem(t *testing.T) {
	gib := func(n int64) *int64 { v := n << 30; return &v }
	yes, no := true, false
	moved := func(vm *vsphere.VM) {
		vm.Files = &vsphere.VMFiles{VMPath: "[ds-b] app/app.vmx", LogDirectory: "[ds-b] app/"}
	}
	for _, tc := range []struct {
		name          string
		before, after func(*vsphere.VM)
		runtime       bool
		want          []FieldChange
	}{
		{"storage vMotion", func(vm *vsphere.VM) {
			vm.Files = &vsphere.VMFiles{VMPath: "[ds-a] app/app.vmx", LogDirectory: "[ds-a] app/"}
		}, moved, false,
			[]FieldChange{{Field: "files.vm_path", Before: "[ds-a] app/app.vmx", After: "[ds-b] app/app.vmx"},
				{Field: "files.log_directory", Before: "[ds-a] app/", After: "[ds-b] app/"}}},
		{"base captured before schema 28", func(*vsphere.VM) {}, moved, true, nil},
		{"directory column missing from an import", func(vm *vsphere.VM) { vm.Files = &vsphere.VMFiles{VMPath: "[ds-b] app/app.vmx"} }, moved, false, nil},
		{"storage grew", func(vm *vsphere.VM) {
			vm.ProvisionedBytes, vm.UnsharedBytes, vm.ConsolidationNeeded = gib(40), gib(0), &no
		},
			func(vm *vsphere.VM) {
				vm.ProvisionedBytes, vm.UnsharedBytes, vm.ConsolidationNeeded = gib(60), gib(2), &yes
			}, true,
			[]FieldChange{{Field: "provisioned_gb", Before: "40.000", After: "60.000"}, {Field: "unshared_gb", Before: "0.000", After: "2.000"},
				{Field: "consolidation_needed", Before: "false", After: "true"}}},
		{"storage grew without runtime fields", func(vm *vsphere.VM) { vm.ProvisionedBytes = gib(40) },
			func(vm *vsphere.VM) { vm.ProvisionedBytes = gib(60) }, false, nil},
		{"storage not read in the base", func(*vsphere.VM) {},
			func(vm *vsphere.VM) {
				vm.ProvisionedBytes, vm.UnsharedBytes, vm.ConsolidationNeeded = gib(60), gib(2), &yes
			}, true, nil},
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
