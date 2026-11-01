package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func newSnapshotAgeTestHistoryDB(t *testing.T) string {
	t.Helper()
	started := time.Now().UTC().Truncate(time.Second)
	vm := vsphere.VM{
		Location:  vsphere.Location{Context: "prod", Datacenter: "dc1", Path: "/dc1/vm/vm-snapshot"},
		ID:        "vm-1",
		Name:      "vm-snapshot",
		Snapshots: []vsphere.VMSnapshot{{ID: "snap-1", Name: "snap-1", CreateTime: started.Add(time.Minute - 90*24*time.Hour)}},
	}
	return seedHistory(t, started, "", assessment.ContextResult{
		Name: "prod", VCenterID: "vc-1", Status: "success",
		VMs: []assessment.Observation{{VCenterID: "vc-1", Context: "prod", VM: vm}},
		Collections: collections(
			assessment.CollectionResult{Kind: "vm", Status: "success", ItemCount: 1},
			assessment.CollectionResult{Kind: "snapshot", Status: "success", ItemCount: 1},
		),
	})
}

func TestAssessmentSnapshotAgeUsesCompactAgeAndKeepsJSON(t *testing.T) {
	dbPath := newSnapshotAgeTestHistoryDB(t)
	stdout, _, err := runCLI(t, dbPath, "analyze", "snapshots")
	if err != nil {
		t.Fatalf("analyze snapshots: %v", err)
	}
	if !strings.Contains(stdout, "12w") || strings.Contains(stdout, "7776000") {
		t.Fatalf("snapshot age should be compactly rendered as 12w, got:\n%s", stdout)
	}

	stdout, _, err = runCLI(t, dbPath, "analyze", "snapshots", "-o", "json")
	if err != nil {
		t.Fatalf("analyze snapshots JSON: %v", err)
	}
	var ages []struct {
		Age int64 `json:"age"`
	}
	if err := json.Unmarshal([]byte(stdout), &ages); err != nil {
		t.Fatalf("decode snapshots JSON: %v\n%s", err, stdout)
	}
	want := int64(90 * 24 * time.Hour)
	if len(ages) != 1 || ages[0].Age != want {
		t.Fatalf("snapshot JSON age = %+v, want duration nanoseconds %d", ages, want)
	}
}

// Issue #524: an age is measured at the capture, not today, so the table says
// which capture and when.
func TestAnalyzeSnapshotsNamesTheCaptureAgesAreMeasuredAt(t *testing.T) {
	captured := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	vm := vsphere.VM{
		Location:  vsphere.Location{Context: "prod", Datacenter: "dc1", Path: "/dc1/vm/vm-snapshot"},
		ID:        "vm-1",
		Name:      "vm-snapshot",
		Snapshots: []vsphere.VMSnapshot{{ID: "snap-1", Name: "snap-1", CreateTime: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}},
	}
	dbPath := seedHistory(t, captured, "", assessment.ContextResult{
		Name: "prod", VCenterID: "vc-1", Status: "success",
		VMs: []assessment.Observation{{VCenterID: "vc-1", Context: "prod", VM: vm}},
		Collections: collections(
			assessment.CollectionResult{Kind: "vm", Status: "success", ItemCount: 1},
			assessment.CollectionResult{Kind: "snapshot", Status: "success", ItemCount: 1},
		),
	})
	stdout, _, err := runCLI(t, dbPath, "analyze", "snapshots")
	if err != nil {
		t.Fatal(err)
	}
	header := "Capture 1 (" + captured.Local().Format("2006-01-02") + "): ages as of the capture\n"
	if !strings.HasPrefix(stdout, header) || !strings.Contains(stdout, " 2w ") {
		t.Fatalf("want a 2w age under %q, got:\n%s", header, stdout)
	}
}

func TestAssessmentSnapshotTrendOldestUsesCompactAge(t *testing.T) {
	dbPath := newSnapshotAgeTestHistoryDB(t)
	stdout, _, err := runCLI(t, dbPath, "analyze", "snapshots", "--trend")
	if err != nil {
		t.Fatalf("analyze snapshots --trend: %v", err)
	}
	if !strings.Contains(stdout, "12w") || strings.Contains(stdout, "7776000") {
		t.Fatalf("snapshot trend oldest age should be compactly rendered as 12w, got:\n%s", stdout)
	}
}

func TestAssessmentSnapshotsRejectsAnInvalidOlderThan(t *testing.T) {
	_, _, err := runCLI(t, newSnapshotAgeTestHistoryDB(t), "analyze", "snapshots", "--older-than", "soon")
	if err == nil || !strings.Contains(err.Error(), "--older-than") {
		t.Fatalf("err = %v, want an --older-than failure", err)
	}
}
