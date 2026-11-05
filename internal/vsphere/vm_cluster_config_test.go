package vsphere_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The rules, group and override are what the nested lab's vCenter 8.0.3
// returns for Cluster-DR, with HA off: keep-together (key 1) and keep-apart
// (key 2) both name vm-normal, keep-apart and the override name
// vm-reservations, and vm-iso is in the VM group of a vm-host rule.
func TestListVMsReadsEachVMsClusterRulesAndHASettings(t *testing.T) {
	model := simulator.VPX()
	model.Machine = 3
	if err := model.Create(); err != nil {
		t.Fatalf("create simulator model: %v", err)
	}
	t.Cleanup(model.Remove)

	m := model.Map()
	cluster := m.Any("ClusterComputeResource").(*simulator.ClusterComputeResource)
	normal := simVM(t, m, "DC0_C0_RP0_VM0")
	reservations := simVM(t, m, "DC0_C0_RP0_VM1")
	iso := simVM(t, m, "DC0_C0_RP0_VM2")
	yes, no := types.NewBool(true), types.NewBool(false)
	cfg := cluster.ConfigurationEx.(*types.ClusterConfigInfoEx)
	cfg.DasConfig.Enabled, cfg.DasConfig.VmMonitoring = no, "vmMonitoringDisabled"
	cfg.DasConfig.DefaultVmSettings = &types.ClusterDasVmSettings{RestartPriority: "medium", IsolationResponse: "none"}
	cfg.Rule = []types.BaseClusterRuleInfo{
		&types.ClusterAffinityRuleSpec{ClusterRuleInfo: types.ClusterRuleInfo{Key: 1, Name: "keep-together", Enabled: yes, Mandatory: no}, Vm: []types.ManagedObjectReference{normal.Self, iso.Self}},
		&types.ClusterAntiAffinityRuleSpec{ClusterRuleInfo: types.ClusterRuleInfo{Key: 2, Name: "keep-apart", Enabled: yes, Mandatory: no}, Vm: []types.ManagedObjectReference{normal.Self, reservations.Self}},
		&types.ClusterVmHostRuleInfo{ClusterRuleInfo: types.ClusterRuleInfo{Key: 3, Name: "iso-on-dr-hosts", Enabled: yes, Mandatory: no}, VmGroupName: "iso-vms", AffineHostGroupName: "dr-hosts"},
	}
	cfg.Group = []types.BaseClusterGroupInfo{
		&types.ClusterHostGroup{ClusterGroupInfo: types.ClusterGroupInfo{Name: "dr-hosts"}, Host: cluster.Host},
		&types.ClusterVmGroup{ClusterGroupInfo: types.ClusterGroupInfo{Name: "iso-vms"}, Vm: []types.ManagedObjectReference{iso.Self}},
	}
	cfg.DasVmConfig = []types.ClusterDasVmConfigInfo{{
		Key: reservations.Self, PowerOffOnIsolation: yes,
		DasSettings: &types.ClusterDasVmSettings{RestartPriority: "high", IsolationResponse: "powerOff",
			VmToolsMonitoringSettings: &types.ClusterVmToolsMonitoringSettings{VmMonitoring: "vmMonitoringOnly", ClusterSettings: yes}},
	}}

	gc, endpoint := dialSimulator(t, model)
	vms, err := clientFor(gc, endpoint, "").ListVMs(context.Background())
	if err != nil {
		t.Fatalf("ListVMs: %v", err)
	}
	defaults := func(rules ...vsphere.VMClusterRule) *vsphere.VMClusterConfig {
		return &vsphere.VMClusterConfig{Rules: rules, HARestartPriority: "medium", HAIsolationResponse: "none", HAVMMonitoring: "vmMonitoringDisabled"}
	}
	together := vsphere.VMClusterRule{Name: "keep-together", Kind: vsphere.RuleAffinity, Enabled: true}
	apart := vsphere.VMClusterRule{Name: "keep-apart", Kind: vsphere.RuleAntiAffinity, Enabled: true}
	want := map[string]*vsphere.VMClusterConfig{
		"DC0_C0_RP0_VM0": defaults(together, apart),
		"DC0_C0_RP0_VM1": {Rules: []vsphere.VMClusterRule{apart}, HARestartPriority: "high", HAIsolationResponse: "powerOff", HAVMMonitoring: "vmMonitoringOnly"},
		"DC0_C0_RP0_VM2": defaults(together, vsphere.VMClusterRule{Name: "iso-on-dr-hosts", Kind: vsphere.RuleVMHost, Enabled: true, HostGroup: "dr-hosts"}),
		// A VM on a standalone host has no cluster to configure it.
		"DC0_H0_VM0": nil,
	}
	for name, w := range want {
		t.Run(name, func(t *testing.T) {
			for _, vm := range vms {
				if vm.Name != name {
					continue
				}
				if !reflect.DeepEqual(vm.ClusterConfig, w) {
					t.Errorf("ClusterConfig = %+v, want %+v", vm.ClusterConfig, w)
				}
				if vm.State == nil || vm.State.DASProtected != nil {
					t.Errorf("State = %+v, want DAS protection unset with HA off", vm.State)
				}
				return
			}
			t.Fatalf("%s not listed", name)
		})
	}
}

func simVM(t *testing.T, m *simulator.Registry, name string) *simulator.VirtualMachine {
	t.Helper()
	for _, e := range m.All("VirtualMachine") {
		if vm := e.(*simulator.VirtualMachine); vm.Name == name {
			return vm
		}
	}
	t.Fatalf("simulator has no VM %s", name)
	return nil
}
