package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func newDecommissionTestHistoryDB(t *testing.T, vm vsphere.VM) string {
	t.Helper()
	return seedHistory(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), assessment.CurrentInventorySchemaVersion, assessment.ContextResult{
		Name: "prod", VCenterID: "vc-prod", Status: "success",
		VMs:         []assessment.Observation{{Context: "prod", VCenterID: "vc-prod", VM: vm}},
		Collections: collections(assessment.CollectionResult{Kind: "vm", Status: "success", ItemCount: 1}),
	})
}

func TestVMDecommissionCheckUsesStoredEvidence(t *testing.T) {
	dbPath := newDecommissionTestHistoryDB(t, vsphere.VM{ID: "vm-1", Name: "app", PowerState: "poweredOn", ConnectionState: "connected", ConfigurationAvailable: true})
	stdout, _, err := runCLI(t, dbPath, "vm", "decommission-check", "app", "--fail-on-blockers", "-o", "json")
	if err == nil || !strings.Contains(err.Error(), "decommission check blocked") {
		t.Fatalf("err = %v, output = %s", err, stdout)
	}
	var result struct {
		Verdict string `json:"verdict"`
		RunID   int64  `json:"run_id"`
	}
	if decodeErr := json.Unmarshal([]byte(stdout), &result); decodeErr != nil {
		t.Fatalf("decode output: %v (%s)", decodeErr, stdout)
	}
	if result.Verdict != "blocked" || result.RunID == 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestVMDecommissionCheckNoMatchIsUnknown(t *testing.T) {
	dbPath := newDecommissionTestHistoryDB(t, vsphere.VM{ID: "vm-1", Name: "app", PowerState: "poweredOff", ConnectionState: "connected", ConfigurationAvailable: true})
	stdout, _, err := runCLI(t, dbPath, "vm", "decommission-check", "missing", "-o", "json")
	if err != nil {
		t.Fatalf("err = %v, output = %s", err, stdout)
	}
	if !strings.Contains(stdout, `"verdict": "unknown"`) || !strings.Contains(stdout, `"id": "identity"`) {
		t.Fatalf("output = %s", stdout)
	}
}
