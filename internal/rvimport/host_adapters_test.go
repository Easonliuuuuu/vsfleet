package rvimport

import (
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
	"github.com/xuri/excelize/v2"
)

// vHost's adapter counts are not stored; they only flag vNIC or vHBA rows
// that did not reach their host.
func TestHostAdapterCountsWarnWhenRowsDisagree(t *testing.T) {
	path := writeFixtureWorkbook(t, func(data *assessment.ExportData) {
		data.Run.InventorySchemaVersion = assessment.CurrentInventorySchemaVersion
		editHost(t, data, "host-a1", func(h *vsphere.Host) {
			h.NICs = []vsphere.HostNIC{{Key: "nic-0", Device: "vmnic0"}, {Key: "nic-1", Device: "vmnic1"}}
			h.HBAs = []vsphere.HostHBA{{Key: "hba-0", Device: "vmhba0"}}
			h.VMKs = []vsphere.HostVMKernel{{Key: "vmk-0", Device: "vmk0"}}
		})
	})
	for _, tc := range []struct {
		name   string
		column string
		value  int
		want   string
	}{
		{"counts agree", "", 0, ""},
		{"vNIC row missing", "# NICs", 3, `vNIC: host "esx-a1" has # NICs 3 on vHost, but 2 attached`},
		{"vHBA row missing", "# HBAs", 2, `vHBA: host "esx-a1" has # HBAs 2 on vHost, but 1 attached`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openFixture(t, path)
			if tc.column != "" {
				setHostCell(t, f, "esx-a1", tc.column, tc.value)
			}
			result, err := Parse(f, Options{})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, w := range result.Report.Warnings {
				if strings.Contains(w, "on vHost, but") {
					got = append(got, w)
				}
			}
			switch {
			case tc.want == "" && len(got) != 0:
				t.Errorf("warnings = %q, want none", got)
			case tc.want != "" && (len(got) != 1 || got[0] != tc.want):
				t.Errorf("warnings = %q, want %q", got, tc.want)
			}
		})
	}
}

func setHostCell(t *testing.T, f *excelize.File, host, header string, value any) {
	t.Helper()
	rows, err := f.GetRows(sheetVHost)
	if err != nil || len(rows) == 0 {
		t.Fatalf("read %s: %v", sheetVHost, err)
	}
	col := -1
	for i, h := range rows[0] {
		if h == header {
			col = i
		}
	}
	if col < 0 {
		t.Fatalf("%s has no %s column", sheetVHost, header)
	}
	for r, row := range rows {
		if len(row) == 0 || row[0] != host {
			continue
		}
		cell, err := excelize.CoordinatesToCellName(col+1, r+1)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.SetCellValue(sheetVHost, cell, value); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("%s has no row for %s", sheetVHost, host)
}
