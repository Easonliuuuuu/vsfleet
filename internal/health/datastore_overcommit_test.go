package health

import (
	"encoding/json"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestDatastoreOvercommitted(t *testing.T) {
	data := func(schema string, d vsphere.Datastore) assessment.ExportData {
		payload, _ := json.Marshal(d)
		return assessment.ExportData{
			Run:       assessment.Run{ID: 9, InventorySchemaVersion: schema},
			Contexts:  []assessment.ContextRun{{Name: "prod", VMStatus: "empty", Collections: []assessment.CollectionRun{{Kind: "vm", Status: "empty"}, {Kind: "datastore", Status: "success"}}}},
			Resources: []assessment.ResourceObservation{{Context: "prod", VCenterID: "vc-1", Kind: "datastore", ID: d.ID, Name: d.Name, Payload: payload}},
		}
	}
	// 100 GiB, 60 GiB used: provisioned is 60 GiB plus uncommitted.
	datastore := func(uncommitted *int64) vsphere.Datastore {
		return vsphere.Datastore{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "ds-1", Name: "thin", Accessible: true, CapacityBytes: 100 << 30, FreeBytes: 40 << 30, UncommittedBytes: uncommitted}
	}
	bytes := func(gib int64) *int64 { v := gib << 30; return &v }
	for _, tc := range []struct {
		name       string
		schema     string
		datastore  vsphere.Datastore
		finding    bool
		wantResult string
	}{
		{"overcommitted", assessment.CurrentInventorySchemaVersion, datastore(bytes(80)), true, "fail"},
		{"exactly full", assessment.CurrentInventorySchemaVersion, datastore(bytes(40)), false, "pass"},
		{"uncommitted not read", assessment.CurrentInventorySchemaVersion, datastore(nil), false, "pass"},
		{"capture predates the field", "30", datastore(bytes(80)), false, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := Evaluate(data(tc.schema, tc.datastore), Options{})
			var got []Finding
			for _, f := range report.Findings {
				if f.Rule == "datastore-overcommitted" {
					got = append(got, f)
				}
			}
			if (len(got) == 1) != tc.finding || len(got) > 1 {
				t.Fatalf("findings = %+v, want finding %v", got, tc.finding)
			}
			if tc.finding {
				f := got[0]
				if f.Object.Kind != "datastore" || f.Severity != SeverityWarning || f.Category != CategoryCapacity {
					t.Errorf("finding = %+v", f)
				}
				if f.Evidence[0].Field != "provisioned_percent" || f.Evidence[0].Observed != "140" {
					t.Errorf("evidence = %+v, want provisioned_percent 140", f.Evidence)
				}
			}
			for _, s := range report.Rules {
				if s.Rule == "datastore-overcommitted" && s.Result != tc.wantResult {
					t.Errorf("result = %q, want %q", s.Result, tc.wantResult)
				}
			}
		})
	}
}
