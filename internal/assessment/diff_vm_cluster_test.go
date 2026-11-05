package assessment

import (
	"reflect"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// A VM's cluster rules and HA settings are configuration: a change shows on
// every diff, and a run that did not read them (before schema 34, or a VM on
// a standalone host) is not compared.
func TestCompareVMsReportsClusterRulesAndHASettings(t *testing.T) {
	apart := vsphere.VMClusterRule{Name: "keep-apart", Kind: vsphere.RuleAntiAffinity, Enabled: true}
	together := vsphere.VMClusterRule{Name: "keep-together", Kind: vsphere.RuleAffinity, Enabled: true}
	lab := func(rules ...vsphere.VMClusterRule) *vsphere.VMClusterConfig {
		return &vsphere.VMClusterConfig{Rules: rules, HARestartPriority: "medium", HAIsolationResponse: "none", HAVMMonitoring: "vmMonitoringDisabled"}
	}
	for _, tc := range []struct {
		name          string
		before, after *vsphere.VMClusterConfig
		want          []FieldChange
	}{
		{"rule added", lab(together), lab(together, apart),
			[]FieldChange{{Field: "cluster_config.rules", Before: "keep-together (affinity)", After: "keep-apart (anti-affinity), keep-together (affinity)"}}},
		{"last rule removed", lab(apart), lab(),
			[]FieldChange{{Field: "cluster_config.rules", Before: "keep-apart (anti-affinity)", After: ""}}},
		{"rules listed in another order", lab(together, apart), lab(apart, together), nil},
		{"rule disabled", lab(apart), lab(vsphere.VMClusterRule{Name: "keep-apart", Kind: vsphere.RuleAntiAffinity}),
			[]FieldChange{{Field: "cluster_config.rule_states", Before: "", After: "keep-apart (disabled)"}}},
		{"VM-host rule added", lab(), lab(vsphere.VMClusterRule{Name: "iso-on-dr-hosts", Kind: vsphere.RuleVMHost, Enabled: true, HostGroup: "dr-hosts"}),
			[]FieldChange{{Field: "cluster_config.host_rules", Before: "", After: "iso-on-dr-hosts (vm-host, on dr-hosts)"}}},
		{"HA override added", lab(), &vsphere.VMClusterConfig{HARestartPriority: "high", HAIsolationResponse: "powerOff", HAVMMonitoring: "vmMonitoringOnly"},
			[]FieldChange{
				{Field: "cluster_config.ha_isolation_response", Before: `"none"`, After: `"powerOff"`},
				{Field: "cluster_config.ha_restart_priority", Before: `"medium"`, After: `"high"`},
				{Field: "cluster_config.ha_vm_monitoring", Before: `"vmMonitoringDisabled"`, After: `"vmMonitoringOnly"`},
			}},
		{"base captured before schema 34", nil, lab(apart), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := stored("app", "vc-a", "vm-1", "instance-1")
			target := base
			base.observation.VM.ClusterConfig, target.observation.VM.ClusterConfig = tc.before, tc.after
			changes, _ := compareVMs([]storedVM{base}, []storedVM{target}, false)
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
