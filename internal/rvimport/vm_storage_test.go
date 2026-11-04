package rvimport

import (
	"reflect"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The "recorded" cells are vapp-web's in the real RVTools 4.8.2.1 export of
// the nested lab, where an unshared size of 0 is a value, not a gap.
func TestVInfoStorageAndFilesAreImported(t *testing.T) {
	path := writeFixtureWorkbook(t, nil)
	columns := []string{"Provisioned MiB", "Unshared MiB", "Consolidation Needed", "Path", "Log directory", "Snapshot directory", "Suspend directory"}
	for _, tc := range []struct {
		name                  string
		cells                 []string
		provisioned, unshared int64
		storage               bool
		consolidation         *bool
		files                 *vsphere.VMFiles
	}{
		{"recorded", []string{"1361", "0", "False", "[local-esxi02] vapp-web/vapp-web.vmx", "[local-esxi02] vapp-web/", "[local-esxi02] vapp-web/", "[local-esxi02] vapp-web/"},
			1361 << 20, 0, true, new(bool),
			&vsphere.VMFiles{VMPath: "[local-esxi02] vapp-web/vapp-web.vmx", LogDirectory: "[local-esxi02] vapp-web/", SnapshotDirectory: "[local-esxi02] vapp-web/", SuspendDirectory: "[local-esxi02] vapp-web/"}},
		{"not reported", []string{"", "", "", "", "", "", ""}, 0, 0, false, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openFixture(t, path)
			row := vInfoRow(t, f, "web-01")
			for i, column := range columns {
				setColumnValue(t, f, sheetVInfo, column, row, tc.cells[i])
			}
			result, err := Parse(f, Options{})
			if err != nil {
				t.Fatal(err)
			}
			vm := findVM(result, "alpha", "web-01")
			if got := vm.ProvisionedBytes != nil && vm.UnsharedBytes != nil; got != tc.storage {
				t.Fatalf("storage recorded = %v, want %v", got, tc.storage)
			}
			if tc.storage && (*vm.ProvisionedBytes != tc.provisioned || *vm.UnsharedBytes != tc.unshared) {
				t.Errorf("provisioned, unshared = %d, %d; want %d, %d", *vm.ProvisionedBytes, *vm.UnsharedBytes, tc.provisioned, tc.unshared)
			}
			if !reflect.DeepEqual(vm.ConsolidationNeeded, tc.consolidation) {
				t.Errorf("consolidation needed = %v, want %v", vm.ConsolidationNeeded, tc.consolidation)
			}
			if !reflect.DeepEqual(vm.Files, tc.files) {
				t.Errorf("files = %+v, want %+v", vm.Files, tc.files)
			}
		})
	}
}
