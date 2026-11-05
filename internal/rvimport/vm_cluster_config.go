package rvimport

import (
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// vmClusterRuleKinds maps vInfo's Cluster rule(s) entries back to rule kinds.
var vmClusterRuleKinds = map[string]string{"affinity": vsphere.RuleAffinity, "anti affinity": vsphere.RuleAntiAffinity}

// applyVMClusterCells reads vInfo's HA and cluster rule columns. A row with
// none of them, as in a workbook without the columns or for a VM on a
// standalone host, leaves ClusterConfig nil. The workbook does not say
// whether a rule is enabled or mandatory, so an imported rule reads as an
// enabled, optional one; only rules between VMs are listed.
func applyVMClusterCells(t sheetTable, row []string, vm *vsphere.VM) {
	cfg := vsphere.VMClusterConfig{
		HARestartPriority:   t.cell(row, "HA Restart Priority"),
		HAIsolationResponse: t.cell(row, "HA Isolation Response"),
		HAVMMonitoring:      t.cell(row, "HA VM Monitoring"),
	}
	kinds, names := splitList(t.cell(row, "Cluster rule(s)")), splitList(t.cell(row, "Cluster rule name(s)"))
	for i, name := range names {
		rule := vsphere.VMClusterRule{Name: name, Enabled: true}
		if i < len(kinds) {
			rule.Kind = vmClusterRuleKinds[strings.ToLower(kinds[i])]
		}
		cfg.Rules = append(cfg.Rules, rule)
	}
	if cfg.HARestartPriority != "" || cfg.HAIsolationResponse != "" || cfg.HAVMMonitoring != "" || len(cfg.Rules) > 0 {
		vm.ClusterConfig = &cfg
	}
}
