package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/sizing"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func newSizingHistoryDB(t *testing.T, datastoreStatus string) string {
	t.Helper()
	const gib = int64(1 << 30)
	ds := vsphere.Datastore{Location: vsphere.Location{Context: "prod", Datacenter: "dc"}, ID: "ds-1", Name: "ds1", CapacityBytes: 1000 * gib, FreeBytes: 500 * gib, Accessible: true, Backing: vsphere.DatastoreBacking{VMFSUUID: "u1"}}
	mk := func(name string, cpu int32) assessment.Observation {
		return assessment.Observation{Context: "prod", VCenterID: "vc-1", VM: vsphere.VM{Location: vsphere.Location{Context: "prod"}, ID: name, Name: name, Cluster: "c1", PowerState: "poweredOn", CPU: cpu, MemoryMB: 16 * 1024, ConfigurationAvailable: true,
			Disks: []vsphere.VMDisk{{Key: 1, Label: "d", CapacityBytes: 100 * gib, BackingPath: "[ds1] " + name + "/" + name + ".vmdk"}}}}
	}
	datastores := assessment.CollectionResult{Kind: "datastore", Status: datastoreStatus, ItemCount: 1}
	if datastoreStatus == "success" {
		datastores.Resources = []assessment.ResourceObservation{resourcePayload(t, "prod", "vc-1", "datastore", "ds-1", "ds1", ds)}
	}
	return seedHistory(t, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), assessment.CurrentInventorySchemaVersion, assessment.ContextResult{
		Name: "prod", VCenterID: "vc-1", Status: "success",
		VMs:         []assessment.Observation{mk("a", 8), mk("b", 8)},
		Collections: collections(assessment.CollectionResult{Kind: "vm", Status: "success", ItemCount: 2}, datastores),
	})
}

func TestSizingCommandFitAndInsufficient(t *testing.T) {
	dbPath := newSizingHistoryDB(t, "success")
	base := []string{"analyze", "sizing", "--run", "latest", "--host-cores", "16", "--host-ram-gib", "64", "--datastore-capacity", "1TiB"}
	stdout, _, err := runCLI(t, dbPath, append(base, "--hosts", "3", "-o", "json")...)
	if err != nil {
		t.Fatal(err)
	}
	var result sizing.Result
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("JSON=%s err=%v", stdout, err)
	}
	if result.Verdict != sizing.VerdictFit || result.Basis != "allocation-based" || result.RunID == 0 {
		t.Fatalf("result=%+v", result)
	}
	// A single host leaves nothing after the default N-1 allowance.
	stdout, _, err = runCLI(t, dbPath, append(base, "--hosts", "1", "--fail-unless-fit")...)
	var coded interface{ ExitCode() int }
	if !errors.As(err, &coded) || coded.ExitCode() != 2 {
		t.Fatalf("err=%v, want exit code 2", err)
	}
	if !strings.Contains(stdout, "INSUFFICIENT") || !strings.Contains(stdout, "allocation-based") || !strings.Contains(stdout, "Target assumptions") {
		t.Fatalf("text output=%s", stdout)
	}
}

func TestSizingCommandMissingInputsAndCoverageAreUnknown(t *testing.T) {
	stdout, _, err := runCLI(t, newSizingHistoryDB(t, "success"), "analyze", "sizing")
	if err != nil || !strings.Contains(stdout, "UNKNOWN") {
		t.Fatalf("err=%v output=%s", err, stdout)
	}
	stdout, _, err = runCLI(t, newSizingHistoryDB(t, "failed"), "analyze", "sizing", "--hosts", "9", "--host-cores", "64", "--host-ram-gib", "1024", "--datastore-capacity", "100TiB")
	if err != nil || !strings.Contains(stdout, "UNKNOWN") || !strings.Contains(stdout, "Coverage gaps") {
		t.Fatalf("err=%v output=%s", err, stdout)
	}
	if _, _, err := runCLI(t, newSizingHistoryDB(t, "success"), "analyze", "sizing", "--datastore-capacity", "lots"); err == nil {
		t.Fatal("invalid --datastore-capacity unexpectedly succeeded")
	}
}
