package report

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The "recorded" row is vapp-web in the real RVTools 4.8.2.1 export of the
// nested lab: an unshared size of zero is written, not left empty.
func TestWriteRVToolsVInfoStorageAndFiles(t *testing.T) {
	columns := []string{"Consolidation Needed", "Provisioned MiB", "Unshared MiB", "Path", "Log directory", "Snapshot directory", "Suspend directory"}
	provisioned, unshared, consolidate := int64(1361<<20), int64(0), false
	for _, tc := range []struct {
		name string
		vm   func(*vsphere.VM)
		want []string
	}{
		{"recorded", func(vm *vsphere.VM) {
			vm.ProvisionedBytes, vm.UnsharedBytes, vm.ConsolidationNeeded = &provisioned, &unshared, &consolidate
			vm.Files = &vsphere.VMFiles{VMPath: "[local-esxi02] vapp-web/vapp-web.vmx", LogDirectory: "[local-esxi02] vapp-web/",
				SnapshotDirectory: "[local-esxi02] vapp-web/", SuspendDirectory: "[local-esxi02] vapp-web/"}
		}, []string{"FALSE", "1361", "0", "[local-esxi02] vapp-web/vapp-web.vmx", "[local-esxi02] vapp-web/", "[local-esxi02] vapp-web/", "[local-esxi02] vapp-web/"}},
		{"captured before schema 28", func(*vsphere.VM) {}, []string{"", "", "", "", "", "", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			tc.vm(&data.VMs[0].Observation.VM)
			var buf bytes.Buffer
			if err := WriteRVTools(&buf, data, healthReport(data)); err != nil {
				t.Fatal(err)
			}
			f, err := excelize.OpenReader(&buf)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			got := sheetCells(t, f, "vInfo", "app", columns)
			for i, column := range columns {
				if got[i] != tc.want[i] {
					t.Errorf("%s = %q, want %q", column, got[i], tc.want[i])
				}
			}
		})
	}
}
