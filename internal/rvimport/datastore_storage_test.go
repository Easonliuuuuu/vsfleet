package rvimport

import "testing"

// nvme-01 in the fixture holds 2 TiB with 1 TiB free, so 1 TiB in use.
func TestVDatastoreProvisionedIsImportedAsUncommitted(t *testing.T) {
	path := writeFixtureWorkbook(t, nil)
	for _, tc := range []struct {
		name        string
		cell        string
		uncommitted int64
		recorded    bool
	}{
		{"overcommitted", "3145728", 2 << 40, true},
		{"rounded below in use", "1048575", 0, true},
		{"not reported", "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openFixture(t, path)
			setColumnValue(t, f, sheetVDatastore, "Provisioned MiB", vmRow(t, f, sheetVDatastore, "nvme-01"), tc.cell)
			result, err := Parse(f, Options{})
			if err != nil {
				t.Fatal(err)
			}
			ds := findDatastore(result, "alpha", "nvme-01")
			if ds == nil {
				t.Fatal("nvme-01 was not imported")
			}
			if got := ds.UncommittedBytes != nil; got != tc.recorded {
				t.Fatalf("uncommitted recorded = %v, want %v", got, tc.recorded)
			}
			if tc.recorded && *ds.UncommittedBytes != tc.uncommitted {
				t.Errorf("uncommitted = %d, want %d", *ds.UncommittedBytes, tc.uncommitted)
			}
		})
	}
}
