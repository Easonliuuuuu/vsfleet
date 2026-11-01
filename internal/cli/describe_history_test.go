package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// Issue #523: two snapshots taken at one time read as one row twice unless each
// event names its snapshot. The name is apart from the time, which stays a plain
// value for -o json and for the interface to format.
func TestDescribeHistoryNamesEachSnapshot(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	result := func(snapshots ...vsphere.VMSnapshot) assessment.ContextResult {
		vm := vsphere.VM{Location: vsphere.Location{Context: "prod"}, ID: "vm-1", Name: "app", InstanceUUID: "instance-1", Snapshots: snapshots}
		return assessment.ContextResult{
			Name: "prod", VCenterID: "vc-1", Status: "success",
			VMs: []assessment.Observation{{VCenterID: "vc-1", Context: "prod", VM: vm}},
			Collections: collections(
				assessment.CollectionResult{Kind: "vm", Status: "success", ItemCount: 1},
				assessment.CollectionResult{Kind: "snapshot", Status: "success", ItemCount: len(snapshots)},
			),
		}
	}
	dbPath := seedHistory(t, created, "", result())
	seedRun(t, dbPath, created.Add(time.Hour), "", result(
		vsphere.VMSnapshot{ID: "snap-1", Name: "before-8.0.3", CreateTime: created},
		vsphere.VMSnapshot{ID: "snap-2", Name: "after-8.0.3", CreateTime: created},
	))

	stdout, _, err := runCLI(t, dbPath, "describe", "vm", "app", "--history", "-A")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"snapshot before-8.0.3:→2026-01-01T00:00:00Z", "snapshot after-8.0.3:→2026-01-01T00:00:00Z"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("history is missing %q:\n%s", want, stdout)
		}
	}

	stdout, _, err = runCLI(t, dbPath, "describe", "vm", "app", "--history", "-A", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"after": "2026-01-01T00:00:00Z"`) {
		t.Errorf("JSON should keep the time as a plain value:\n%s", stdout)
	}
}
