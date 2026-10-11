package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/health"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func newHealthTestHistoryDB(t *testing.T, findings bool) string {
	t.Helper()
	vm := vsphere.VM{ID: "vm-1", Name: "app", PowerState: "poweredOn", ToolsState: "guestToolsNotRunning"}
	if !findings {
		vm.PowerState = "poweredOff"
		vm.ToolsState = "guestToolsRunning"
	} else {
		vm.ConnectionState = "orphaned"
	}
	return seedVMHistory(t, vm, nil)
}

// seedVMHistory stores one run in which prod holds the single VM vm.
func seedVMHistory(t *testing.T, vm vsphere.VM, collected []assessment.CollectionResult) string {
	t.Helper()
	return seedHistory(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), assessment.CurrentInventorySchemaVersion, assessment.ContextResult{
		Name: "prod", VCenterID: "vc-uuid", Status: "success",
		VMs:         []assessment.Observation{{Context: "prod", VCenterID: "vc-uuid", VM: vm}},
		Collections: collected,
	})
}

func newOrphanTestHistoryDB(t *testing.T) string {
	t.Helper()
	return newOrphanCoverageHistoryDB(t, "10", vsphere.Datastore{Location: vsphere.Location{Context: "prod"}, ID: "ds-1", Name: "datastore1", Accessible: true, BrowseStatus: "success", Files: []vsphere.DatastoreFile{{Path: "[datastore1] lost/orphan.vmdk", SizeBytes: 8 << 30}}})
}

func newOrphanCoverageHistoryDB(t *testing.T, schema string, datastore vsphere.Datastore) string {
	t.Helper()
	return seedHistory(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), schema, assessment.ContextResult{Name: "prod", VCenterID: "vc-1", Status: "success", Collections: []assessment.CollectionResult{
		{Kind: "vm", Status: "success"},
		{Kind: "datastore", Status: "success", Resources: []assessment.ResourceObservation{resourcePayload(t, "prod", "vc-1", "datastore", datastore.ID, datastore.Name, datastore)}},
	}})
}

func TestAssessmentOrphansReportsMissingBrowseEvidence(t *testing.T) {
	strong := vsphere.DatastoreBacking{VMFSUUID: "uuid-1"}

	// Captured without --browse-datastores: an empty candidate list must not
	// read as a clean estate.
	noBrowse := newOrphanCoverageHistoryDB(t, "11", vsphere.Datastore{Location: vsphere.Location{Context: "prod"}, ID: "ds-1", Name: "datastore1", Accessible: true, Backing: strong})
	stdout, stderr, err := runCLI(t, noBrowse, "assessment", "orphans", "latest")
	if err != nil {
		t.Fatalf("orphans err=%v", err)
	}
	if strings.Contains(stdout, "No browsed VMDK orphan candidates.") || !strings.Contains(stdout, "NOT EVALUATED") {
		t.Fatalf("no-browse stdout=%q", stdout)
	}
	if !strings.Contains(stderr, "not-browsed") {
		t.Fatalf("no-browse stderr=%q", stderr)
	}

	// --fail-on-unknown makes the incomplete coverage a non-zero exit.
	_, _, gateErr := runCLI(t, noBrowse, "assessment", "orphans", "latest", "--fail-on-unknown")
	var coded interface{ ExitCode() int }
	if !errors.As(gateErr, &coded) || coded.ExitCode() != 2 {
		t.Fatalf("fail-on-unknown err=%v code=%v", gateErr, coded)
	}

	// JSON exposes coverage even with entries == [].
	jsonOut, _, err := runCLI(t, noBrowse, "assessment", "orphans", "latest", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Entries  []any `json:"entries"`
		Coverage struct {
			Datastores int `json:"datastores"`
			Browsed    int `json:"browsed"`
			Gaps       []struct {
				Status string `json:"status"`
			} `json:"gaps"`
		} `json:"coverage"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("invalid orphans JSON: %v\n%s", err, jsonOut)
	}
	if len(report.Entries) != 0 || report.Coverage.Browsed != 0 || len(report.Coverage.Gaps) != 1 || report.Coverage.Gaps[0].Status != "not-browsed" {
		t.Fatalf("coverage JSON=%+v", report.Coverage)
	}

	// A truncated listing with zero candidates is still not a clean result.
	truncated := newOrphanCoverageHistoryDB(t, "11", vsphere.Datastore{Location: vsphere.Location{Context: "prod"}, ID: "ds-2", Name: "datastore2", Accessible: true, Backing: strong, BrowseStatus: "success", BrowseTruncated: true})
	stdout, _, err = runCLI(t, truncated, "assessment", "orphans", "latest")
	if err != nil || strings.Contains(stdout, "No browsed VMDK orphan candidates.") || !strings.Contains(stdout, "NOT EVALUATED") {
		t.Fatalf("truncated err=%v stdout=%q", err, stdout)
	}

	// A fully browsed, genuinely clean estate still prints a clean result and exits 0.
	clean := newOrphanCoverageHistoryDB(t, "11", vsphere.Datastore{Location: vsphere.Location{Context: "prod"}, ID: "ds-3", Name: "datastore3", Accessible: true, Backing: strong, BrowseStatus: "success"})
	stdout, _, err = runCLI(t, clean, "assessment", "orphans", "latest", "--fail-on-unknown")
	if err != nil {
		t.Fatalf("clean estate failed: %v", err)
	}
	if !strings.Contains(stdout, "No browsed VMDK orphan candidates.") {
		t.Fatalf("clean stdout=%q", stdout)
	}
}

func TestAssessmentOrphansCommandAndConfidenceFiltering(t *testing.T) {
	db := newOrphanTestHistoryDB(t)
	stdout, _, err := runCLI(t, db, "assessment", "orphans", "latest")
	if err != nil || !strings.Contains(stdout, "SUSPECTED") || !strings.Contains(stdout, "orphan.vmdk") {
		t.Fatalf("orphans output err=%v output=%s", err, stdout)
	}
	// The stored path already carries "[datastore]"; the header must not add a second one.
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "[SUSPECTED]") && strings.Count(line, "] ") != 2 {
			t.Fatalf("orphan header should name the datastore once: %q", line)
		}
	}
	stdout, _, err = runCLI(t, db, "assessment", "orphans", "latest", "-o", "json", "--confidence", string(health.ConfidenceSuspected))
	if err != nil || !strings.Contains(stdout, `"confidence": "suspected-unreferenced"`) {
		t.Fatalf("orphans JSON err=%v output=%s", err, stdout)
	}
	if healthOut, healthErr, gateErr := runCLI(t, db, "health", "latest", "--severity", "warning", "--fail-on-findings"); gateErr != nil {
		t.Fatalf("suspected-only estate failed warning gate: %v stdout=%s stderr=%s", gateErr, healthOut, healthErr)
	}
}

func TestHealthCommandExitCodes(t *testing.T) {
	findingsDB := newHealthTestHistoryDB(t, true)
	stdout, _, err := runCLI(t, findingsDB, "health", "latest", "--fail-on-findings")
	var coded interface{ ExitCode() int }
	if !errors.As(err, &coded) || coded.ExitCode() != 2 {
		t.Fatalf("health findings error=%v, code=%v, output=%s", err, coded, stdout)
	}
	if !strings.Contains(stdout, "tools-not-running") {
		t.Fatalf("health output=%s", stdout)
	}
	if !strings.Contains(stdout, "vm-orphaned") {
		t.Fatalf("health output omitted orphaned VM finding=%s", stdout)
	}

	cleanDB := newHealthTestHistoryDB(t, false)
	if _, _, err := runCLI(t, cleanDB, "health", "latest", "--fail-on-findings"); err != nil {
		t.Fatalf("clean health returned %v", err)
	}
	if _, _, err := runCLI(t, cleanDB, "health", "does-not-exist"); err == nil {
		t.Fatal("bad selector unexpectedly succeeded")
	}
}

func TestHealthCommandJSONAndRuleListing(t *testing.T) {
	db := newHealthTestHistoryDB(t, false)
	stdout, stderr, err := runCLI(t, db, "health", "latest", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("invalid health JSON: %v\n%s", err, stdout)
	}
	if report["run_id"] != float64(1) {
		t.Fatalf("health JSON=%v", report)
	}
	if !strings.Contains(stderr, "datastore-zombie-vmdk") {
		t.Fatalf("health output did not explain skipped datastore browsing: %s", stderr)
	}

	stdout, _, err = runCLI(t, db, "health", "--list-rules")
	if err != nil || !strings.Contains(stdout, "datastore-inaccessible") || !strings.Contains(stdout, "datastore-zombie-vmdk") || !strings.Contains(stdout, "vm-orphaned") || !strings.Contains(stdout, "tools-outdated") {
		t.Fatalf("rule listing err=%v output=%s", err, stdout)
	}
}

func TestAssessmentReadinessBlocksAndHasExitCode(t *testing.T) {
	connected := true
	vm := vsphere.VM{ID: "vm-1", Name: "app", PowerState: "poweredOn", CDROMs: []vsphere.VMCDROM{{Label: "installer", Connected: &connected}}}
	dbPath := seedVMHistory(t, vm, []assessment.CollectionResult{{Kind: "vm", Status: "success", ItemCount: 1}})
	stdout, _, err := runCLI(t, dbPath, "assessment", "readiness", "latest", "--fail-on-blockers")
	var coded interface{ ExitCode() int }
	if !errors.As(err, &coded) || coded.ExitCode() != 2 {
		t.Fatalf("readiness error=%v, code=%v, output=%s", err, coded, stdout)
	}
	if !strings.Contains(stdout, "BLOCKED") || !strings.Contains(stdout, "cdrom-connected") {
		t.Fatalf("readiness output=%s", stdout)
	}
}

func TestHealthCommandReportsConnectedDevicesFromStoredEvidence(t *testing.T) {
	connected := true
	vm := vsphere.VM{ID: "vm-1", Name: "app", CDROMs: []vsphere.VMCDROM{{Key: 301, Label: "CD/DVD drive 1", Connected: &connected, BackingType: "iso", BackingPath: "[ds] app/install.iso"}}, USBs: []vsphere.VMUSB{{Key: 401, Label: "USB device 1", Connected: &connected, BackingType: "remoteHost", BackingHost: "esx-1"}}}
	dbPath := seedVMHistory(t, vm, []assessment.CollectionResult{{Kind: "vm", Status: "success", ItemCount: 1}})
	stdout, _, err := runCLI(t, dbPath, "health", "latest", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"rule": "cdrom-connected"`) || !strings.Contains(stdout, `"rule": "usb-connected"`) || !strings.Contains(stdout, "install.iso") || !strings.Contains(stdout, "esx-1") {
		t.Fatalf("health JSON missing connected-device findings: %s", stdout)
	}
}
