package health

import (
	"encoding/json"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestStorageDRSSpaceThreshold(t *testing.T) {
	sdrs := &vsphere.StorageDRS{Enabled: true}
	// member is a 100 GiB datastore of pod-a with free GiB free.
	member := func(id string, free int64, sdrs *vsphere.StorageDRS) vsphere.Datastore {
		return vsphere.Datastore{Location: vsphere.Location{Datacenter: "dc-a"}, ID: id, Name: id, Accessible: true, CapacityBytes: 100 << 30, FreeBytes: free << 30,
			Details: &vsphere.DatastoreDetails{StorageCluster: &vsphere.DatastoreCluster{ID: "group-p1", Name: "pod-a", StorageDRS: sdrs}}}
	}
	data := func(schema string, members ...vsphere.Datastore) assessment.ExportData {
		out := assessment.ExportData{
			Run:      assessment.Run{ID: 9, InventorySchemaVersion: schema},
			Contexts: []assessment.ContextRun{{Name: "prod", VMStatus: "empty", Collections: []assessment.CollectionRun{{Kind: "vm", Status: "empty"}, {Kind: "datastore", Status: "success"}}}},
		}
		for _, d := range members {
			payload, _ := json.Marshal(d)
			out.Resources = append(out.Resources, assessment.ResourceObservation{Context: "prod", VCenterID: "vc-1", Kind: "datastore", ID: d.ID, Name: d.Name, Payload: payload})
		}
		return out
	}
	current := assessment.CurrentInventorySchemaVersion
	inaccessible := member("ds-3", 50, sdrs)
	inaccessible.Accessible = false
	for _, tc := range []struct {
		name       string
		data       assessment.ExportData
		wantResult string
		message    string
	}{
		{"every member past 80% used", data(current, member("ds-1", 15, sdrs), member("ds-2", 19, sdrs)), "fail",
			"Every datastore in the cluster is past Storage DRS's 80% used space threshold (2 of 2)"},
		{"one member with room", data(current, member("ds-1", 15, sdrs), member("ds-2", 21, sdrs)), "pass", ""},
		{"an inaccessible member counts as past it", data(current, member("ds-1", 15, sdrs), inaccessible), "fail",
			"Every datastore in the cluster is past Storage DRS's 80% used space threshold (2 of 2)"},
		{"free-space mode", data(current, member("ds-1", 40, &vsphere.StorageDRS{Enabled: true, SpaceThresholdMode: "freeSpace", FreeSpaceGB: 50})), "fail",
			"Every datastore in the cluster is past Storage DRS's 50 GB free space threshold (1 of 1)"},
		{"Storage DRS off", data(current, member("ds-1", 5, &vsphere.StorageDRS{})), "pass", ""},
		{"configuration not read", data(current, member("ds-1", 5, nil)), "pass", ""},
		{"capture predates the field", data("37", member("ds-1", 5, sdrs)), "unknown", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := Evaluate(tc.data, Options{})
			var got []Finding
			for _, f := range report.Findings {
				if f.Rule == "storage-drs-space-threshold" {
					got = append(got, f)
				}
			}
			if tc.message == "" && len(got) != 0 {
				t.Fatalf("findings = %+v, want none", got)
			}
			if tc.message != "" {
				if len(got) != 1 {
					t.Fatalf("findings = %+v, want one", got)
				}
				if f := got[0]; f.Message != tc.message || f.Object.Kind != "datastorecluster" || f.Object.ID != "group-p1" || f.Object.Name != "pod-a" || f.Object.Datacenter != "dc-a" || f.Severity != SeverityWarning {
					t.Errorf("finding = %+v", f)
				}
			}
			for _, status := range report.Rules {
				if status.Rule == "storage-drs-space-threshold" && status.Result != tc.wantResult {
					t.Errorf("result = %q, want %q", status.Result, tc.wantResult)
				}
			}
		})
	}
}
