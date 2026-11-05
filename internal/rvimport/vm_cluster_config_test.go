package rvimport

import (
	"reflect"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// A VM's rules between VMs and its HA settings import back as a vsfleet
// export wrote them; the VM-host rule, which vInfo does not list, does not.
// The values are the nested lab's vm-normal and vm-reservations.
func TestVMClusterConfigRoundTrips(t *testing.T) {
	yes := true
	together := vsphere.VMClusterRule{Name: "keep-together", Kind: vsphere.RuleAffinity, Enabled: true}
	apart := vsphere.VMClusterRule{Name: "keep-apart", Kind: vsphere.RuleAntiAffinity, Enabled: true}
	for _, tc := range []struct {
		name         string
		config, want *vsphere.VMClusterConfig
		das          *bool
	}{
		{"vm-normal",
			&vsphere.VMClusterConfig{Rules: []vsphere.VMClusterRule{together, apart, {Name: "iso-on-dr-hosts", Kind: vsphere.RuleVMHost, Enabled: true, HostGroup: "dr-hosts"}},
				HARestartPriority: "medium", HAIsolationResponse: "none", HAVMMonitoring: "vmMonitoringDisabled"},
			&vsphere.VMClusterConfig{Rules: []vsphere.VMClusterRule{together, apart}, HARestartPriority: "medium", HAIsolationResponse: "none", HAVMMonitoring: "vmMonitoringDisabled"}, nil},
		{"vm-reservations, protected by HA",
			&vsphere.VMClusterConfig{Rules: []vsphere.VMClusterRule{apart}, HARestartPriority: "high", HAIsolationResponse: "powerOff", HAVMMonitoring: "vmMonitoringOnly"},
			&vsphere.VMClusterConfig{Rules: []vsphere.VMClusterRule{apart}, HARestartPriority: "high", HAIsolationResponse: "powerOff", HAVMMonitoring: "vmMonitoringOnly"}, &yes},
		{"captured before schema 34", nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openFixture(t, writeFixtureWorkbook(t, func(data *assessment.ExportData) {
				vm := &data.VMs[0].Observation.VM
				vm.ClusterConfig = tc.config
				if tc.das != nil {
					vm.State = &vsphere.VMState{DASProtected: tc.das}
				}
			}))
			result, err := Parse(f, Options{})
			if err != nil {
				t.Fatal(err)
			}
			vm := findVM(result, "alpha", "web-01")
			if !reflect.DeepEqual(vm.ClusterConfig, tc.want) {
				t.Errorf("ClusterConfig = %+v, want %+v", vm.ClusterConfig, tc.want)
			}
			var das *bool
			if vm.State != nil {
				das = vm.State.DASProtected
			}
			if !reflect.DeepEqual(das, tc.das) {
				t.Errorf("DAS protection = %v, want %v", das, tc.das)
			}
		})
	}
}
