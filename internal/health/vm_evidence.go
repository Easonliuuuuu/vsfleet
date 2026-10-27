package health

import (
	"fmt"
	"sort"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// PartialCoverage is a context a rule evaluated without every VM: Unevaluated
// of its Total non-template VMs had no evidence for the rule and were left out.
type PartialCoverage struct {
	Context     string `json:"context"`
	Unevaluated int    `json:"unevaluated_vms"`
	Total       int    `json:"total_vms"`
}

// resolveVMEvidence scopes missing VM evidence to the VMs that lack it. A
// context where no non-template VM has the evidence (an RVTools import, or a
// capture that predates the fields) is blind. Anywhere else the rule still
// judges every VM that has it, and the rest are counted as partial coverage:
// one inaccessible VM, which vCenter returns no configuration for, must not
// blind the whole context.
func resolveVMEvidence(in Input, needs string, missing func(vsphere.VM) bool) (result, reason string, blind []string, partial []PartialCoverage) {
	type tally struct{ total, missing int }
	contexts := make(map[string]*tally)
	for _, item := range in.Data.VMs {
		vm := item.Observation.VM
		if vm.IsTemplate {
			continue
		}
		name := contextName(item.Observation)
		t := contexts[name]
		if t == nil {
			t = &tally{}
			contexts[name] = t
		}
		t.total++
		if missing(vm) {
			t.missing++
		}
	}
	var reasons []string
	for name, t := range contexts {
		switch {
		case t.missing == 0:
		case t.missing == t.total:
			blind = append(blind, name)
		default:
			partial = append(partial, PartialCoverage{Context: name, Unevaluated: t.missing, Total: t.total})
		}
	}
	sort.Strings(blind)
	sort.Slice(partial, func(i, j int) bool { return partial[i].Context < partial[j].Context })
	if len(blind) > 0 {
		result = "unknown"
		reasons = append(reasons, needs+" evidence is incomplete")
	}
	if len(partial) > 0 {
		counts := make([]string, 0, len(partial))
		for _, p := range partial {
			counts = append(counts, fmt.Sprintf("%d of %d in %s", p.Unevaluated, p.Total, p.Context))
		}
		reasons = append(reasons, "VMs without "+needs+" evidence were not evaluated: "+strings.Join(counts, ", "))
	}
	return result, strings.Join(reasons, "; "), blind, partial
}

// noConfiguration reports a VM whose configuration vCenter did not return.
// An inaccessible or orphaned VM has none; the vm-inaccessible and
// vm-orphaned rules report the VM itself.
func noConfiguration(vm vsphere.VM) bool {
	return !vm.ConfigurationAvailable
}

func noFirmware(vm vsphere.VM) bool {
	return noConfiguration(vm) || !validFirmware(vm.Firmware)
}

func noResourceAllocation(vm vsphere.VM) bool {
	return noConfiguration(vm) || vm.CPUAllocation == nil || vm.MemoryAllocation == nil
}

func noCPUTopology(vm vsphere.VM) bool {
	return noConfiguration(vm) || vm.CPU <= 0 || vm.CoresPerSocket <= 0
}

func noSecureBoot(vm vsphere.VM) bool {
	return noFirmware(vm) || strings.EqualFold(vm.Firmware, "efi") && vm.SecureBootEnabled == nil
}

func validFirmware(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "bios") || strings.EqualFold(strings.TrimSpace(value), "efi")
}

// noToolsInstallation reports a VM with neither Tools version status nor the
// legacy installation status RVTools writes. Either says whether Tools are
// installed.
func noToolsInstallation(vm vsphere.VM) bool {
	return vm.ToolsVersionStatus == "" && vm.ToolsStatus == ""
}

// noToolsRunningStatus reports a VM with no Tools running status, such as one
// imported from a workbook that carries neither Tools Running Status nor a
// Tools value that implies it.
func noToolsRunningStatus(vm vsphere.VM) bool {
	return vm.ToolsState == ""
}
