package report

import (
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The values are vm-normal's and vm-reservations' in the real RVTools
// 4.8.2.1 export of the nested lab, with HA off in Cluster-DR. RVTools lists
// the rules between VMs in the cluster's order and leaves out vm-iso's
// VM-host rule.
func TestWriteRVToolsVMClusterRulesAndHASettings(t *testing.T) {
	yes := true
	together := vsphere.VMClusterRule{Name: "keep-together", Kind: vsphere.RuleAffinity, Enabled: true}
	apart := vsphere.VMClusterRule{Name: "keep-apart", Kind: vsphere.RuleAntiAffinity, Enabled: true}
	onHosts := vsphere.VMClusterRule{Name: "iso-on-dr-hosts", Kind: vsphere.RuleVMHost, Enabled: true, HostGroup: "dr-hosts"}
	defaults := func(rules ...vsphere.VMClusterRule) *vsphere.VMClusterConfig {
		return &vsphere.VMClusterConfig{Rules: rules, HARestartPriority: "medium", HAIsolationResponse: "none", HAVMMonitoring: "vmMonitoringDisabled"}
	}
	headers := []string{"DAS protection", "HA Restart Priority", "HA Isolation Response", "HA VM Monitoring", "Cluster rule(s)", "Cluster rule name(s)"}
	for _, tc := range []struct {
		name   string
		config *vsphere.VMClusterConfig
		das    *bool
		want   []string
	}{
		{"vm-normal", defaults(together, apart), nil, []string{"", "medium", "none", "vmMonitoringDisabled", "Affinity, Anti Affinity", "keep-together, keep-apart"}},
		{"vm-reservations", &vsphere.VMClusterConfig{Rules: []vsphere.VMClusterRule{apart}, HARestartPriority: "high", HAIsolationResponse: "powerOff", HAVMMonitoring: "vmMonitoringOnly"}, nil,
			[]string{"", "high", "powerOff", "vmMonitoringOnly", "Anti Affinity", "keep-apart"}},
		{"vm-iso", defaults(onHosts), nil, []string{"", "medium", "none", "vmMonitoringDisabled", "", ""}},
		{"protected by HA", defaults(), &yes, []string{"TRUE", "medium", "none", "vmMonitoringDisabled", "", ""}},
		{"captured before schema 34", nil, nil, []string{"", "", "", "", "", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			vm := &data.VMs[0].Observation.VM
			vm.ClusterConfig = tc.config
			if tc.das != nil {
				vm.State = &vsphere.VMState{DASProtected: tc.das}
			}
			got := firstRowByHeader(t, data, "vInfo")
			for i, h := range headers {
				if cell, found := got[h]; !found || cell != tc.want[i] {
					t.Errorf("%s = %q (column present: %v), want %q", h, cell, found, tc.want[i])
				}
			}
		})
	}
}
