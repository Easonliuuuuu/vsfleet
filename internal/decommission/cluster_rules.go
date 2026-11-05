package decommission

import (
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
)

// clusterRuleCheck reports the enabled DRS rules that name the VM. Removing
// the VM changes what they enforce for the VMs left in them, and an
// affinity rule's partner may depend on it, so the result is advisory. A
// capture that did not read the rules leaves it not assessed rather than
// unknown, so it never holds back a verdict.
func clusterRuleCheck(observations []assessment.ExportVM) Check {
	check := Check{ID: "cluster-rules", Impact: ImpactAdvisory}
	var names []string
	var evidence []Evidence
	for _, item := range observations {
		vm := item.Observation.VM
		if vm.ClusterConfig == nil {
			if vm.Cluster != "" || !vm.ConfigurationAvailable {
				check.Status, check.Message = StatusNotAssessed, "cluster rules were not collected by this capture"
				return check
			}
			continue
		}
		for _, r := range vm.ClusterConfig.Rules {
			if r.Enabled {
				names = append(names, r.Name)
				evidence = append(evidence, Evidence{Field: "cluster_rule", Observed: r.Name + " (" + r.Kind + ")"})
			}
		}
	}
	if len(names) == 0 {
		check.Status, check.Message = StatusPass, "no enabled cluster rule names the VM"
		return check
	}
	check.Status, check.Evidence = StatusFail, evidence
	check.Message = "VM is named in cluster rule " + strings.Join(names, ", ") + "; review what the rule enforces for its other VMs before removing it"
	return check
}
