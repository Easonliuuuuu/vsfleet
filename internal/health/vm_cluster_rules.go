package health

import (
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// evaluateVMClusterRules reports a VM named in an enabled DRS rule. The rule
// belongs to the VM's cluster, so it does not follow the VM: migrating the
// VM alone breaks an affinity rule's intent or frees it from an
// anti-affinity one. A disabled rule constrains nothing and is left out.
func evaluateVMClusterRules(in Input, emit func(Finding)) {
	for _, item := range in.Data.VMs {
		vm := item.Observation.VM
		if vm.IsTemplate || vm.ClusterConfig == nil {
			continue
		}
		var rules []string
		var evidence []Evidence
		for _, r := range vm.ClusterConfig.Rules {
			if !r.Enabled {
				continue
			}
			rules = append(rules, r.Name)
			evidence = append(evidence, Evidence{Field: "cluster_rule", Observed: r.Name + " (" + clusterRuleDescription(r) + ")"})
		}
		if len(rules) == 0 {
			continue
		}
		emit(Finding{Object: vmObject(in.Data, item.Observation), Message: "VM is bound by cluster rule " + strings.Join(rules, ", "), Evidence: evidence})
	}
}

// clusterRuleDescription says what a rule requires, e.g. "mandatory
// vm-host, on dr-hosts".
func clusterRuleDescription(r vsphere.VMClusterRule) string {
	kind := r.Kind
	if r.Mandatory {
		kind = "mandatory " + kind
	}
	if r.HostGroup == "" {
		return kind
	}
	if r.HostAntiAffine {
		return kind + ", off " + r.HostGroup
	}
	return kind + ", on " + r.HostGroup
}

// noClusterConfig reports a VM whose cluster rules were not read: one in a
// cluster, or one without its configuration, whose placement is unknown. A
// VM on a standalone host has no cluster rules to read.
func noClusterConfig(vm vsphere.VM) bool {
	return vm.ClusterConfig == nil && (vm.Cluster != "" || noConfiguration(vm))
}
