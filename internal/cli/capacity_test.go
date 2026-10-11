package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestCapacityCommandJSONAndFilters(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	stdout, _, err := runCLI(t, dbPath, "assessment", "capacity", "latest", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var report assessment.CapacityReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("JSON=%s err=%v", stdout, err)
	}
	if len(report.Datastores) != 2 || report.SchemaVersion != 1 {
		t.Fatalf("report=%+v", report)
	}
	stdout, _, err = runCLI(t, dbPath, "assessment", "capacity", "latest", "--datastore", "missing", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(stdout), []byte(`"datastores": []`)) {
		t.Fatalf("filtered report=%s", stdout)
	}
	if _, _, err := runCLI(t, dbPath, "assessment", "capacity", "latest", "--min-free-bytes", "not-a-size"); err == nil {
		t.Fatal("invalid --min-free-bytes unexpectedly succeeded")
	}
	if _, _, err := runCLI(t, dbPath, "assessment", "capacity", "latest", "--to", "latest"); err == nil {
		t.Fatal("positional RUN and --to unexpectedly succeeded")
	}
}

func newProjectionCapacityHistoryDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "history.db")
	for i := 0; i < 8; i++ {
		ds := vsphere.Datastore{Location: vsphere.Location{Context: "prod", Datacenter: "dc-a"}, ID: "ds-1", Name: "prod", CapacityBytes: 2000, FreeBytes: int64(1000 - i*100)}
		seedRun(t, dbPath, time.Date(2026, 6, 1+i, 0, 0, 0, 0, time.UTC), assessment.CurrentInventorySchemaVersion, assessment.ContextResult{
			Name: "prod", VCenterID: "vc-1", Status: "success",
			Collections: collections(
				assessment.CollectionResult{Kind: "vm", Status: "success"},
				assessment.CollectionResult{Kind: "datastore", Status: "success", ItemCount: 1, Resources: []assessment.ResourceObservation{resourcePayload(t, "prod", "vc-1", "datastore", "ds-1", "prod", ds)}},
			),
		})
	}
	return dbPath
}

func TestCapacityCommandFailOnProjection(t *testing.T) {
	dbPath := newProjectionCapacityHistoryDB(t)
	_, _, err := runCLI(t, dbPath, "assessment", "capacity", "latest", "--since", "10d", "--fail-on-projection", "30d")
	var coded interface{ ExitCode() int }
	if !errors.As(err, &coded) || coded.ExitCode() != 2 {
		t.Fatalf("err=%v, want exit code 2", err)
	}
}
