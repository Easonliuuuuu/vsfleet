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
	stdout, _, err := runCLI(t, dbPath, "assessment", "snapshots")
	if err != nil {
		t.Fatalf("assessment snapshots: %v", err)
	}
	if !strings.Contains(stdout, "12w") || strings.Contains(stdout, "7776000") {
		t.Fatalf("snapshot age should be compactly rendered as 12w, got:\n%s", stdout)
	}

	stdout, _, err = runCLI(t, dbPath, "assessment", "snapshots", "-o", "json")
	if err != nil {
		t.Fatalf("assessment snapshots JSON: %v", err)
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

func TestAssessmentSnapshotTrendOldestUsesCompactAge(t *testing.T) {
	dbPath := newSnapshotAgeTestHistoryDB(t)
	stdout, _, err := runCLI(t, dbPath, "assessment", "trends", "snapshots")
	if err != nil {
		t.Fatalf("assessment snapshot trend: %v", err)
	}
	if !strings.Contains(stdout, "12w") || strings.Contains(stdout, "7776000") {
		t.Fatalf("snapshot trend oldest age should be compactly rendered as 12w, got:\n%s", stdout)
	}
}

func TestAssessmentSnapshotsRejectsAnInvalidOlderThan(t *testing.T) {
	_, _, err := runCLI(t, newSnapshotAgeTestHistoryDB(t), "assessment", "snapshots", "--older-than", "soon")
	if err == nil || !strings.Contains(err.Error(), "--older-than") {
		t.Fatalf("err = %v, want an --older-than failure", err)
	}
}
