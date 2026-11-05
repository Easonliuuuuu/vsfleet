package decommission

import (
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// A VM named in an enabled cluster rule gets an advisory that names the
// rule. It never blocks, and a capture without the rules leaves it not
// assessed rather than unknown. The rules are the nested lab's vm-normal's.
func TestEvaluateReportsAVMBoundByAClusterRule(t *testing.T) {
	apart := vsphere.VMClusterRule{Name: "keep-apart", Kind: vsphere.RuleAntiAffinity, Enabled: true}
	together := vsphere.VMClusterRule{Name: "keep-together", Kind: vsphere.RuleAffinity, Enabled: true}
	for _, tc := range []struct {
		name    string
		cluster string
		config  *vsphere.VMClusterConfig
		status  string
		message string
	}{
		{"two rules", "Cluster-DR", &vsphere.VMClusterConfig{Rules: []vsphere.VMClusterRule{together, apart}}, StatusFail,
			"VM is named in cluster rule keep-together, keep-apart; review what the rule enforces for its other VMs before removing it"},
		{"disabled rule", "Cluster-DR", &vsphere.VMClusterConfig{Rules: []vsphere.VMClusterRule{{Name: "keep-apart", Kind: vsphere.RuleAntiAffinity}}}, StatusPass, "no enabled cluster rule names the VM"},
		{"standalone host", "", nil, StatusPass, "no enabled cluster rule names the VM"},
		{"captured before schema 34", "Cluster-DR", nil, StatusNotAssessed, "cluster rules were not collected by this capture"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := vsphere.VM{ID: "vm-1009", Name: "vm-normal", Cluster: tc.cluster, PowerState: "poweredOff", ConnectionState: "connected", ConfigurationAvailable: true, ClusterConfig: tc.config}
			report := Evaluate(baseData(vm), "vm-normal", nil)
			check := findCheck(report.Subjects[0], "cluster-rules")
			if check.Status != tc.status || check.Impact != ImpactAdvisory || check.Message != tc.message {
				t.Errorf("check = %+v, want %s: %s", check, tc.status, tc.message)
			}
		})
	}
}
