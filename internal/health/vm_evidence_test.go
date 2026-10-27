package health

import (
	"reflect"
	"slices"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// TestMissingVMConfigurationIsScopedToTheVM covers issue #404: one VM that
// vCenter returned no configuration for (an inaccessible VM) blinded every
// migration-configuration rule for its whole context.
func TestMissingVMConfigurationIsScopedToTheVM(t *testing.T) {
	configured := func(id string, floppy bool) vsphere.VM {
		secureBoot, unlimited := false, int64(-1)
		vm := vsphere.VM{Location: vsphere.Location{Context: "prod", Datacenter: "dc-a"}, ID: id, Name: id, ConfigurationAvailable: true,
			ToolsState: "guestToolsRunning", ToolsVersionStatus: "guestToolsCurrent",
			Firmware: "efi", SecureBootEnabled: &secureBoot, CPU: 2, CoresPerSocket: 1,
			CPUAllocation: &vsphere.VMResourceAllocation{Limit: &unlimited}, MemoryAllocation: &vsphere.VMResourceAllocation{Limit: &unlimited}}
		if floppy {
			vm.Floppies = []vsphere.VMFloppy{{Key: 1, Label: "floppy", BackingType: "image"}}
		}
		return vm
	}
	unconfigured := func(id, state string) vsphere.VM {
		return vsphere.VM{Location: vsphere.Location{Context: "prod", Datacenter: "dc-a"}, ID: id, Name: id, PowerState: "poweredOff", ConnectionState: state}
	}
	tests := []struct {
		name        string
		vms         []vsphere.VM
		wantResult  string
		wantBlind   []string
		wantPartial []PartialCoverage
	}{
		{
			name:        "one inaccessible VM among configured VMs",
			vms:         []vsphere.VM{configured("app", true), configured("db", false), unconfigured("lost", "inaccessible")},
			wantResult:  "fail",
			wantPartial: []PartialCoverage{{Context: "prod", Unevaluated: 1, Total: 3}},
		},
		{
			// An RVTools import never records VM configuration.
			name:       "no VM has configuration",
			vms:        []vsphere.VM{unconfigured("app", "connected"), unconfigured("db", "connected")},
			wantResult: "unknown",
			wantBlind:  []string{"prod"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := assessment.ExportData{
				Run:      assessment.Run{ID: 1, InventorySchemaVersion: assessment.CurrentInventorySchemaVersion},
				Contexts: []assessment.ContextRun{{Name: "prod", VMStatus: "success", Collections: []assessment.CollectionRun{{Kind: "vm", Status: "success"}}}},
			}
			for _, vm := range tc.vms {
				data.VMs = append(data.VMs, assessment.ExportVM{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-1", VM: vm}})
			}
			report := Evaluate(data, Options{})

			scoped := make(map[string]bool)
			for _, rule := range Rules() {
				scoped[rule.ID] = rule.MissingEvidence != nil
			}
			checked := 0
			for _, status := range report.Rules {
				if !scoped[status.Rule] {
					continue
				}
				checked++
				if !slices.Equal(status.Blind, tc.wantBlind) || !reflect.DeepEqual(status.Partial, tc.wantPartial) {
					t.Errorf("%s = blind %v partial %+v (%s); want blind %v partial %+v", status.Rule, status.Blind, status.Partial, status.Reason, tc.wantBlind, tc.wantPartial)
				}
				if status.Rule == "floppy-present" && status.Result != tc.wantResult {
					t.Errorf("floppy-present result = %q, want %q", status.Result, tc.wantResult)
				}
			}
			if checked != 13 {
				t.Fatalf("checked %d VM evidence rules, want the 11 from issue #404 and the 2 Tools rules from #490", checked)
			}
			// A VM left out is still missing evidence: readiness must not
			// call the estate ready.
			if verdict := Readiness(report).Verdict; verdict != "unknown" {
				t.Errorf("readiness verdict = %q, want unknown", verdict)
			}
		})
	}
}
