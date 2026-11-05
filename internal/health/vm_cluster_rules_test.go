package health

import (
	"reflect"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// A VM named in an enabled rule is a migration advisory. The rules are the
// nested lab's: vm-normal is in keep-together and keep-apart, and vm-iso in
// a VM-host rule.
func TestVMClusterRuleIsAReadinessAdvisory(t *testing.T) {
	together := vsphere.VMClusterRule{Name: "keep-together", Kind: vsphere.RuleAffinity, Enabled: true}
	apart := vsphere.VMClusterRule{Name: "keep-apart", Kind: vsphere.RuleAntiAffinity, Enabled: true}
	for _, tc := range []struct {
		name         string
		schema       string
		config       *vsphere.VMClusterConfig
		wantEvidence []string
		wantResult   string
	}{
		{"two rules between VMs", assessment.CurrentInventorySchemaVersion, &vsphere.VMClusterConfig{Rules: []vsphere.VMClusterRule{together, apart}},
			[]string{"keep-together (affinity)", "keep-apart (anti-affinity)"}, "fail"},
		{"mandatory VM-host rule", assessment.CurrentInventorySchemaVersion,
			&vsphere.VMClusterConfig{Rules: []vsphere.VMClusterRule{{Name: "iso-on-dr-hosts", Kind: vsphere.RuleVMHost, Enabled: true, Mandatory: true, HostGroup: "dr-hosts"}}},
			[]string{"iso-on-dr-hosts (mandatory vm-host, on dr-hosts)"}, "fail"},
		{"disabled rule", assessment.CurrentInventorySchemaVersion, &vsphere.VMClusterConfig{Rules: []vsphere.VMClusterRule{{Name: "keep-apart", Kind: vsphere.RuleAntiAffinity}}}, nil, "pass"},
		{"no rules", assessment.CurrentInventorySchemaVersion, &vsphere.VMClusterConfig{HARestartPriority: "medium"}, nil, "pass"},
		{"rules not read", assessment.CurrentInventorySchemaVersion, nil, nil, "unknown"},
		{"capture predates the field", "33", nil, nil, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := vsphere.VM{ID: "vm-1009", Name: "vm-normal", Cluster: "Cluster-DR", PowerState: "poweredOff", ConfigurationAvailable: true, CPU: 1, MemoryMB: 256, ClusterConfig: tc.config}
			data := assessment.ExportData{
				Run:      assessment.Run{ID: 9, InventorySchemaVersion: tc.schema},
				Contexts: []assessment.ContextRun{{Name: "lab", VMStatus: "success", Collections: []assessment.CollectionRun{{Kind: "vm", Status: "success"}}}},
				VMs:      []assessment.ExportVM{{Observation: assessment.Observation{Context: "lab", VCenterID: "vc-1", VM: vm}}},
			}
			report := Evaluate(data, Options{})
			var evidence []string
			for _, f := range report.Findings {
				if f.Rule != "vm-cluster-rule" {
					continue
				}
				if f.Category != CategoryMigration || f.Severity != SeverityInfo || f.Object.Name != "vm-normal" {
					t.Errorf("finding = %+v", f)
				}
				for _, e := range f.Evidence {
					evidence = append(evidence, e.Observed)
				}
			}
			if !reflect.DeepEqual(evidence, tc.wantEvidence) {
				t.Errorf("evidence = %q, want %q", evidence, tc.wantEvidence)
			}
			for _, s := range report.Rules {
				if s.Rule == "vm-cluster-rule" && s.Result != tc.wantResult {
					t.Errorf("result = %q, want %q", s.Result, tc.wantResult)
				}
			}
			advisories := 0
			for _, f := range Readiness(report).Advisories {
				if f.Rule == "vm-cluster-rule" {
					advisories++
				}
			}
			if want := len(tc.wantEvidence) > 0; (advisories == 1) != want {
				t.Errorf("readiness advisories for the rule = %d, want one: %v", advisories, want)
			}
		})
	}
}
