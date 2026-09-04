package assessment

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func testContext(name string) *config.Context {
	return &config.Context{Name: name, Endpoint: "https://" + name, Username: "user"}
}

func testVM(name, moref, instance, bios, host string) vsphere.VM {
	return vsphere.VM{Location: vsphere.Location{Context: "prod", Datacenter: "dc1", Path: "/dc1/vm/" + name}, ID: moref, Name: name, InstanceUUID: instance, BIOSUUID: bios, CPU: 2, MemoryMB: 4096, Host: host, Cluster: "cluster-a", Datastores: []string{"ds-a"}}
}

func saveTestRun(t *testing.T, s *Store, when time.Time, vm vsphere.VM) Run {
	t.Helper()
	r, err := s.StartRun(context.Background(), "test", []*config.Context{testContext("prod")}, when)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveContext(context.Background(), r.ID, ContextResult{Name: "prod", VCenterID: "vc-1", Status: "success", VMs: []Observation{{VCenterID: "vc-1", Context: "prod", VM: vm}}}, when.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	r, err = s.FinishRun(context.Background(), r.ID, when.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestStoreRoundTripAndDiff(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	v1 := testVM("billing", "vm-1", "instance-1", "bios-1", "esx-1")
	r1 := saveTestRun(t, s, base, v1)
	v2 := testVM("billing", "vm-1", "instance-1", "bios-1", "esx-2")
	v2.CPU = 4
	v2.Snapshots = []vsphere.VMSnapshot{{ID: "snap-1", Name: "before-upgrade", CreateTime: base.Add(-24 * time.Hour), PowerState: "poweredOn"}}
	r2 := saveTestRun(t, s, base.Add(2*time.Hour), v2)
	d, err := s.Diff(context.Background(), r1.ID, r2.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if d.Counts.Moved != 1 || d.Counts.Modified != 1 || d.Counts.Snapshots != 1 {
		t.Fatalf("counts=%+v", d.Counts)
	}
	if len(d.SnapshotAges) != 1 || d.SnapshotAges[0].Age != 26*time.Hour+time.Second {
		t.Fatalf("ages=%+v", d.SnapshotAges)
	}
	h, err := s.History(context.Background(), "billing", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 2 {
		t.Fatalf("history len=%d", len(h))
	}
	if err := s.DeleteRun(context.Background(), r1.ID); err != nil {
		t.Fatal(err)
	}
	if runs, _ := s.Runs(context.Background()); len(runs) != 1 {
		t.Fatalf("runs after delete=%d", len(runs))
	}
}

func TestStoreDoesNotClaimLifecycleForFailedContext(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r1 := saveTestRun(t, s, when, testVM("vm", "vm-1", "i", "b", "esx"))
	r2, err := s.StartRun(context.Background(), "test", []*config.Context{testContext("prod")}, when.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveContext(context.Background(), r2.ID, ContextResult{Name: "prod", Status: "failed", Error: "unreachable"}, when.Add(time.Hour+time.Second)); err != nil {
		t.Fatal(err)
	}
	r2, err = s.FinishRun(context.Background(), r2.ID, when.Add(time.Hour+time.Second))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Diff(context.Background(), r1.ID, r2.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if d.Counts.Vanished != 0 || len(d.Warnings) == 0 {
		t.Fatalf("diff=%+v", d)
	}
	if !strings.Contains(d.Warnings[0], "not fully collected") {
		t.Fatalf("warnings=%v", d.Warnings)
	}
}

func TestStoreRecoversInterruptedRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.StartRun(context.Background(), "test", []*config.Context{testContext("prod")}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetRun(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != RunFailed {
		t.Fatalf("status=%s", got.Status)
	}
}

// TestOpenAppliesPragmasToEveryConnection guards the pool rather than the
// first connection. The pragmas are connection-scoped, so running them through
// sql.DB reaches only whichever connection served that call and leaves the
// rest of the pool with busy_timeout=0 and foreign_keys=OFF.
func TestOpenAppliesPragmasToEveryConnection(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	// Hold every connection open at once so each iteration is forced to open a
	// new one instead of reusing the connection Open already touched.
	var conns []*sql.Conn
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	for i := 0; i < 4; i++ {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
		conns = append(conns, conn)

		var foreignKeys int
		if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatalf("connection %d foreign_keys: %v", i, err)
		}
		if foreignKeys != 1 {
			t.Errorf("connection %d: foreign_keys = %d, want 1", i, foreignKeys)
		}

		var busyTimeout int
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
			t.Fatalf("connection %d busy_timeout: %v", i, err)
		}
		if busyTimeout != 5000 {
			t.Errorf("connection %d: busy_timeout = %d, want 5000", i, busyTimeout)
		}

		var journalMode string
		if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
			t.Fatalf("connection %d journal_mode: %v", i, err)
		}
		if !strings.EqualFold(journalMode, "wal") {
			t.Errorf("connection %d: journal_mode = %q, want wal", i, journalMode)
		}
	}
}

// TestPruneCascadesObservations covers what foreign_keys=OFF silently broke:
// deleting a run has to take its observations with it.
func TestPruneCascadesObservations(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	saveTestRun(t, s, time.Now().UTC().Add(-72*time.Hour), testVM("vm-a", "vm-1", "inst-1", "bios-1", "esx-1"))

	// Occupy the connection Open itself used. Without this the pool hands the
	// delete back that same connection, which carries foreign_keys=ON however
	// it was set, and the test passes even when the rest of the pool does not.
	busy, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	var before int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM vm_observations").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before == 0 {
		t.Fatal("expected stored observations before pruning")
	}

	if _, err := s.Prune(ctx, time.Hour, 0, true); err != nil {
		t.Fatal(err)
	}

	var after int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM vm_observations").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != 0 {
		t.Errorf("vm_observations = %d after pruning every run, want 0 (cascade did not fire)", after)
	}
}
