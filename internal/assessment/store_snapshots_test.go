package assessment

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// saveSnapshotRun stores one VM with a snapshot that records its files and
// one, as an RVTools workbook without the columns gives, that does not.
func saveSnapshotRun(t *testing.T, s *Store) int64 {
	t.Helper()
	ctx := context.Background()
	when := time.Date(2026, 11, 5, 1, 27, 0, 0, time.UTC)
	run, err := s.StartRunWithMetadata(ctx, "test", []*config.Context{{Name: "lab", Endpoint: "https://vc.example"}}, when, RunMetadata{InventorySchemaVersion: CurrentInventorySchemaVersion})
	if err != nil {
		t.Fatal(err)
	}
	data, total := int64(9651), int64(475)
	vm := vsphere.VM{ID: "vm-1011", Name: "vm-snapshot", Snapshots: []vsphere.VMSnapshot{
		{ID: "snapshot-1017", Name: "snap-1", CreateTime: when.Add(-time.Hour), Filename: "[nfs-shared] vm-snapshot/vm-snapshot-Snapshot1.vmsn", DataBytes: &data, TotalBytes: &total},
		{ID: "snapshot-1018", Name: "snap-2", CreateTime: when},
	}}
	if err := s.SaveContext(ctx, run.ID, ContextResult{Name: "lab", VCenterID: "vc-uuid", Status: "success", VMs: []Observation{{Context: "lab", VCenterID: "vc-uuid", VM: vm}},
		Collections: []CollectionResult{{Kind: "vm", Status: "success", ItemCount: 1}}}, when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishRun(ctx, run.ID, when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	return run.ID
}

func exportedSnapshots(t *testing.T, s *Store, runID int64) []vsphere.VMSnapshot {
	t.Helper()
	data, err := s.LoadExportData(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.VMs) != 1 || len(data.VMs[0].Snapshots) != 2 {
		t.Fatalf("exported VMs = %+v, want one VM with two snapshots", data.VMs)
	}
	return data.VMs[0].Snapshots
}

// A snapshot's state file and sizes are stored with it, and one that recorded
// none reads back as not recorded, not as zero bytes.
func TestSnapshotFilesAreStoredWithTheSnapshot(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	snaps := exportedSnapshots(t, s, saveSnapshotRun(t, s))
	if got := snaps[0]; got.Filename != "[nfs-shared] vm-snapshot/vm-snapshot-Snapshot1.vmsn" || got.DataBytes == nil || *got.DataBytes != 9651 || got.TotalBytes == nil || *got.TotalBytes != 475 {
		t.Errorf("snap-1 = %q, %v, %v, want its file, 9651 and 475 bytes", got.Filename, got.DataBytes, got.TotalBytes)
	}
	if got := snaps[1]; got.Filename != "" || got.DataBytes != nil || got.TotalBytes != nil {
		t.Errorf("snap-2 = %q, %v, %v, want nothing recorded", got.Filename, got.DataBytes, got.TotalBytes)
	}
}

// A database written by the previous build (schema 8, no snapshot file
// columns) must open, gain the columns, and report its snapshots' files as
// not recorded.
func TestOpenMigratesSchemaEightWithoutInventingSnapshotSizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	runID := saveSnapshotRun(t, s)
	for _, stmt := range []string{
		`ALTER TABLE snapshot_observations DROP COLUMN filename`,
		`ALTER TABLE snapshot_observations DROP COLUMN data_bytes`,
		`ALTER TABLE snapshot_observations DROP COLUMN total_bytes`,
		`PRAGMA user_version = 8`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopening a schema 8 database: %v", err)
	}
	defer s.Close()
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != currentSchemaVersion {
		t.Fatalf("user_version=%d err=%v, want %d", version, err, currentSchemaVersion)
	}
	for _, got := range exportedSnapshots(t, s, runID) {
		if got.Filename != "" || got.DataBytes != nil || got.TotalBytes != nil {
			t.Errorf("%s after migration = %q, %v, %v, want nothing recorded", got.Name, got.Filename, got.DataBytes, got.TotalBytes)
		}
	}
}
