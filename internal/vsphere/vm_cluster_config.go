package vsphere

import (
	"context"
	"strings"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// VMClusterConfig is what a VM's cluster configures for it. These are
// migration constraints: a rule says which VMs must move or stay together,
// and the HA settings say how the VM is restarted. Nil means not read: a
// capture predating inventory schema 34, a VM on a standalone host, or a
// cluster whose configuration the account could not read.
type VMClusterConfig struct {
	// Rules are the cluster's DRS rules that name the VM, in the order the
	// cluster lists them. Empty means none.
	Rules []VMClusterRule `json:"rules,omitempty"`
	// HARestartPriority, HAIsolationResponse and HAVMMonitoring are the HA
	// settings that apply to the VM: its own override, or the cluster's
	// default when it has none. vSphere keeps them while HA is off.
	HARestartPriority   string `json:"ha_restart_priority,omitempty"`
	HAIsolationResponse string `json:"ha_isolation_response,omitempty"`
	HAVMMonitoring      string `json:"ha_vm_monitoring,omitempty"`
}

// The kinds of DRS rule a VM can be named in.
const (
	// RuleAffinity keeps the rule's VMs on the same host.
	RuleAffinity = "affinity"
	// RuleAntiAffinity keeps the rule's VMs on different hosts.
	RuleAntiAffinity = "anti-affinity"
	// RuleVMHost keeps the VM group on (or, with HostAntiAffine, off) a
	// host group.
	RuleVMHost = "vm-host"
	// RuleDependency starts the VM group only after another group.
	RuleDependency = "dependency"
)

// VMClusterRule is one DRS rule that names the VM.
type VMClusterRule struct {
	Name string `json:"name"`
	// Kind is one of the Rule constants.
	Kind      string `json:"kind"`
	Enabled   bool   `json:"enabled"`
	Mandatory bool   `json:"mandatory"`
	// HostGroup is a vm-host rule's host group, and HostAntiAffine reports
	// that the VM must stay off it rather than on it.
	HostGroup      string `json:"host_group,omitempty"`
	HostAntiAffine bool   `json:"host_anti_affine,omitempty"`
}

// VMVMRule reports a rule between VMs, affinity or anti-affinity: the kind
// RVTools lists in vInfo.
func (r VMClusterRule) VMVMRule() bool {
	return r.Kind == RuleAffinity || r.Kind == RuleAntiAffinity
}

// clusterVMConfigs is the cluster configuration of each VM, by VM. It reads
// configurationEx, which holds the rules, groups and HA overrides; vSphere
// has no narrower path to them. The read is best effort: a cluster whose
// configuration cannot be read leaves its VMs' ClusterConfig nil.
type clusterVMConfigs struct {
	byCluster map[types.ManagedObjectReference]*types.ClusterConfigInfoEx
}

func (c *Client) clusterVMConfigs(ctx context.Context, idx *index) clusterVMConfigs {
	var raw []mo.ClusterComputeResource
	if err := retrieve(ctx, c, idx.root, []string{"ClusterComputeResource"}, []string{"ClusterComputeResource"}, []string{"configurationEx"}, &raw); err != nil {
		return clusterVMConfigs{}
	}
	out := clusterVMConfigs{byCluster: make(map[types.ManagedObjectReference]*types.ClusterConfigInfoEx, len(raw))}
	for i := range raw {
		if cfg, ok := raw[i].ConfigurationEx.(*types.ClusterConfigInfoEx); ok && cfg != nil {
			out.byCluster[raw[i].Self] = cfg
		}
	}
	return out
}

// forVM returns the configuration the cluster of the VM's host applies to
// it, nil when the host is not in a cluster or its configuration was not
// read.
func (cc clusterVMConfigs) forVM(idx *index, vm types.ManagedObjectReference, host *types.ManagedObjectReference) *VMClusterConfig {
	if host == nil {
		return nil
	}
	e, ok := idx.byRef[*host]
	if !ok || e.parent == nil || e.parent.Type != "ClusterComputeResource" {
		return nil
	}
	cfg := cc.byCluster[*e.parent]
	if cfg == nil {
		return nil
	}
	return vmClusterConfig(cfg, vm)
}

// vmClusterConfig resolves the rules naming vm and the HA settings that
// apply to it from its cluster's configuration.
func vmClusterConfig(cfg *types.ClusterConfigInfoEx, vm types.ManagedObjectReference) *VMClusterConfig {
	out := &VMClusterConfig{HAVMMonitoring: cfg.DasConfig.VmMonitoring}
	if d := cfg.DasConfig.DefaultVmSettings; d != nil {
		out.HARestartPriority, out.HAIsolationResponse = d.RestartPriority, d.IsolationResponse
	}
	for _, v := range cfg.DasVmConfig {
		if v.Key == vm {
			applyDASOverride(out, v)
		}
	}
	groups := vmGroups(cfg.Group, vm)
	for _, r := range cfg.Rule {
		if rule, ok := vmRule(r, vm, groups); ok {
			out.Rules = append(out.Rules, rule)
		}
	}
	return out
}

// applyDASOverride applies a VM's own HA settings. A value of
// clusterRestartPriority or clusterIsolationResponse keeps the cluster's
// default. The monitoring settings' clusterSettings flag covers only their
// failure intervals: the lab's vCenter 8.0.3 sets it on an override whose
// vmMonitoring RVTools reports as the VM's own.
func applyDASOverride(out *VMClusterConfig, v types.ClusterDasVmConfigInfo) {
	priority, isolation := string(v.RestartPriority), ""
	if v.PowerOffOnIsolation != nil {
		isolation = map[bool]string{true: "powerOff", false: "none"}[*v.PowerOffOnIsolation]
	}
	if s := v.DasSettings; s != nil {
		if s.RestartPriority != "" {
			priority = s.RestartPriority
		}
		if s.IsolationResponse != "" {
			isolation = s.IsolationResponse
		}
		if m := s.VmToolsMonitoringSettings; m != nil && m.VmMonitoring != "" {
			out.HAVMMonitoring = m.VmMonitoring
		}
	}
	if priority != "" && priority != string(types.ClusterDasVmSettingsRestartPriorityClusterRestartPriority) {
		out.HARestartPriority = priority
	}
	if isolation != "" && isolation != string(types.ClusterDasVmSettingsIsolationResponseClusterIsolationResponse) {
		out.HAIsolationResponse = isolation
	}
}

// vmGroups returns the names of the cluster's VM groups that hold vm.
func vmGroups(groups []types.BaseClusterGroupInfo, vm types.ManagedObjectReference) map[string]bool {
	out := map[string]bool{}
	for _, g := range groups {
		if vg, ok := g.(*types.ClusterVmGroup); ok && vg != nil {
			for _, member := range vg.Vm {
				if member == vm {
					out[vg.Name] = true
				}
			}
		}
	}
	return out
}

// vmRule returns r as it applies to vm, and false when r does not name it.
func vmRule(r types.BaseClusterRuleInfo, vm types.ManagedObjectReference, groups map[string]bool) (VMClusterRule, bool) {
	info := r.GetClusterRuleInfo()
	if info == nil {
		return VMClusterRule{}, false
	}
	rule := VMClusterRule{Name: strings.TrimSpace(info.Name), Enabled: info.Enabled != nil && *info.Enabled, Mandatory: info.Mandatory != nil && *info.Mandatory}
	names := func(vms []types.ManagedObjectReference) bool {
		for _, member := range vms {
			if member == vm {
				return true
			}
		}
		return false
	}
	switch t := r.(type) {
	case *types.ClusterAffinityRuleSpec:
		rule.Kind = RuleAffinity
		return rule, names(t.Vm)
	case *types.ClusterAntiAffinityRuleSpec:
		rule.Kind = RuleAntiAffinity
		return rule, names(t.Vm)
	case *types.ClusterVmHostRuleInfo:
		rule.Kind, rule.HostGroup = RuleVMHost, t.AffineHostGroupName
		if t.AntiAffineHostGroupName != "" {
			rule.HostGroup, rule.HostAntiAffine = t.AntiAffineHostGroupName, true
		}
		return rule, groups[t.VmGroupName]
	case *types.ClusterDependencyRuleInfo:
		rule.Kind = RuleDependency
		return rule, groups[t.VmGroup]
	}
	return VMClusterRule{}, false
}
