package health

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestHAFailoverCapacity(t *testing.T) {
	resources := func(ha *vsphere.ClusterHA) assessment.ExportData {
		payload, _ := json.Marshal(vsphere.Cluster{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "domain-c1", Name: "compute", Hosts: 4, HAEnabled: true, HA: ha})
		return assessment.ExportData{
			Run:       assessment.Run{ID: 7, InventorySchemaVersion: assessment.CurrentInventorySchemaVersion},
			Contexts:  []assessment.ContextRun{{Name: "prod", VMStatus: "empty", Collections: []assessment.CollectionRun{{Kind: "vm", Status: "empty"}, {Kind: "cluster", Status: "success"}}}},
			Resources: []assessment.ResourceObservation{{Context: "prod", VCenterID: "vc-1", Kind: "cluster", ID: "domain-c1", Name: "compute", Payload: payload}},
		}
	}
	percent := func(admission bool, cpu, mem int32) *vsphere.ClusterHA {
		return &vsphere.ClusterHA{AdmissionControl: admission, Policy: vsphere.HAPolicyResources, CPUReservePct: 25, MemReservePct: 25, CPUFailoverPct: cpu, MemFailoverPct: mem}
	}
	hosts := func(current int32) *vsphere.ClusterHA {
		return &vsphere.ClusterHA{AdmissionControl: true, Policy: vsphere.HAPolicyHostFailures, FailoverLevel: 1, CurrentFailoverLevel: current}
	}
	for _, tc := range []struct {
		name         string
		ha           *vsphere.ClusterHA
		wantSeverity Severity // empty: no finding
		wantEvidence []string
		wantResult   string
	}{
		{"memory below reserve refuses power-ons", percent(true, 40, 11), SeverityCritical, []string{"memory_failover_percent", "admission_control"}, "fail"},
		{"both below reserve", percent(true, 10, 11), SeverityCritical, []string{"cpu_failover_percent", "memory_failover_percent", "admission_control"}, "fail"},
		{"admission control off only warns", percent(false, 40, 11), SeverityWarning, []string{"memory_failover_percent", "admission_control"}, "fail"},
		{"at the reserve passes", percent(true, 25, 25), "", nil, "pass"},
		{"unreported capacity is unknown", percent(true, 0, 0), "", nil, "unknown"},
		{"host failures below configured", hosts(0), SeverityCritical, []string{"host_failures_tolerated", "admission_control"}, "fail"},
		{"host failures met", hosts(1), "", nil, "pass"},
		{"dedicated failover hosts are not compared", &vsphere.ClusterHA{AdmissionControl: true, Policy: vsphere.HAPolicyFailoverHosts, FailoverLevel: 1}, "", nil, "pass"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := Evaluate(resources(tc.ha), Options{})
			var got []Finding
			for _, f := range report.Findings {
				if f.Rule == "ha-failover-capacity-low" {
					got = append(got, f)
				}
			}
			for _, s := range report.Rules {
				if s.Rule == "ha-failover-capacity-low" && s.Result != tc.wantResult {
					t.Errorf("result = %q (%s), want %q", s.Result, s.Reason, tc.wantResult)
				}
			}
			if tc.wantSeverity == "" {
				if len(got) != 0 {
					t.Fatalf("findings = %+v, want none", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("findings = %+v, want one", got)
			}
			f := got[0]
			if f.Severity != tc.wantSeverity || f.Object.Kind != "cluster" || f.Object.Name != "compute" || f.Category != CategoryAvailability {
				t.Errorf("finding = %+v", f)
			}
			var fields []string
			for _, e := range f.Evidence {
				fields = append(fields, e.Field)
			}
			if !slices.Equal(fields, tc.wantEvidence) {
				t.Errorf("evidence fields = %v, want %v", fields, tc.wantEvidence)
			}
		})
	}
}
