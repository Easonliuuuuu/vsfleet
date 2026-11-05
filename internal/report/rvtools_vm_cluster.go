package report

import (
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// vmClusterRuleKinds are the rule kinds as RVTools writes them in vInfo's
// Cluster rule(s).
var vmClusterRuleKinds = map[string]string{vsphere.RuleAffinity: "Affinity", vsphere.RuleAntiAffinity: "Anti Affinity"}

// vmClusterCells writes vInfo's HA Restart Priority, HA Isolation Response,
// HA VM Monitoring, Cluster rule(s) and Cluster rule name(s). RVTools lists
// only the rules between VMs, in the order the cluster lists them, so a
// VM-host or dependency rule is left out here; vsfleet keeps it for capture
// diff and analysis. Every cell is empty when the VM's cluster configuration
// was not read.
func vmClusterCells(vm vsphere.VM) []any {
	cfg := vm.ClusterConfig
	if cfg == nil {
		return make([]any, 5)
	}
	var kinds, names []string
	for _, r := range cfg.Rules {
		if r.VMVMRule() {
			kinds, names = append(kinds, vmClusterRuleKinds[r.Kind]), append(names, r.Name)
		}
	}
	return []any{optionalString(cfg.HARestartPriority), optionalString(cfg.HAIsolationResponse), optionalString(cfg.HAVMMonitoring),
		optionalString(strings.Join(kinds, ", ")), optionalString(strings.Join(names, ", "))}
}
