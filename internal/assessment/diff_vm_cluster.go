package assessment

import (
	"sort"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The cluster rule fields a diff reports. RVTools' vInfo lists the rules
// between VMs by name and kind only, so an import carries clusterRulesField
// but never the other two; see importUncarriedFields.
const (
	clusterRulesField     = "cluster_config.rules"
	clusterHostRulesField = "cluster_config.host_rules"
	clusterRuleStateField = "cluster_config.rule_states"
)

// vmClusterConfigChanges reports a change in the DRS rules that name a VM
// or in the HA settings that apply to it. Both are configuration, compared
// on every diff, and only when both runs read them. Each rule field is
// compared as one sorted set, so a rule added or removed is reported rather
// than lost to positional comparison, and a reordering is not a change.
func vmClusterConfigChanges(a, b *vsphere.VMClusterConfig) []FieldChange {
	if a == nil || b == nil {
		return nil
	}
	var out []FieldChange
	for _, field := range []struct {
		name string
		text func([]vsphere.VMClusterRule) string
	}{{clusterHostRulesField, hostRulesText}, {clusterRulesField, vmRulesText}, {clusterRuleStateField, ruleStatesText}} {
		if before, after := field.text(a.Rules), field.text(b.Rules); before != after {
			out = append(out, FieldChange{Field: field.name, Before: before, After: after})
		}
	}
	ha := func(c *vsphere.VMClusterConfig) vsphere.VMClusterConfig {
		return vsphere.VMClusterConfig{HARestartPriority: c.HARestartPriority, HAIsolationResponse: c.HAIsolationResponse, HAVMMonitoring: c.HAVMMonitoring}
	}
	return append(out, recordedFieldChanges("cluster_config", ha(a), ha(b))...)
}

// vmRulesText lists the rules between VMs, e.g. "keep-apart (anti-affinity),
// keep-together (affinity)".
func vmRulesText(rules []vsphere.VMClusterRule) string {
	return rulesText(rules, func(r vsphere.VMClusterRule) string {
		if !r.VMVMRule() {
			return ""
		}
		return r.Name + " (" + r.Kind + ")"
	})
}

// hostRulesText lists the VM-host and dependency rules, e.g.
// "iso-on-dr-hosts (vm-host, on dr-hosts)".
func hostRulesText(rules []vsphere.VMClusterRule) string {
	return rulesText(rules, func(r vsphere.VMClusterRule) string {
		if r.VMVMRule() {
			return ""
		}
		if r.HostGroup == "" {
			return r.Name + " (" + r.Kind + ")"
		}
		return r.Name + " (" + r.Kind + ", " + map[bool]string{false: "on ", true: "off "}[r.HostAntiAffine] + r.HostGroup + ")"
	})
}

// ruleStatesText lists the rules that are disabled or mandatory, e.g.
// "keep-apart (disabled)"; an enabled, optional rule is left out.
func ruleStatesText(rules []vsphere.VMClusterRule) string {
	return rulesText(rules, func(r vsphere.VMClusterRule) string {
		var states []string
		if !r.Enabled {
			states = append(states, "disabled")
		}
		if r.Mandatory {
			states = append(states, "mandatory")
		}
		if len(states) == 0 {
			return ""
		}
		return r.Name + " (" + strings.Join(states, ", ") + ")"
	})
}

func rulesText(rules []vsphere.VMClusterRule, entry func(vsphere.VMClusterRule) string) string {
	var parts []string
	for _, r := range rules {
		if e := entry(r); e != "" {
			parts = append(parts, e)
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
