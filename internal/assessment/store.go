package assessment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

const (
	EnvDBPath = "VSFLEET_HISTORY_DB"
	Driver    = "sqlite"
)

type Store struct{ db *sql.DB }

func DefaultPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv(EnvDBPath)); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, "vsfleet", "history.db"), nil
}

func Open(path string) (*Store, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create history directory: %w", err)
	}
	db, err := sql.Open(Driver, path)
	if err != nil {
		return nil, fmt.Errorf("open history database: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	for _, stmt := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure history database: %w", err)
		}
	}
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	// A process can disappear after creating a run but before finalizing it.
	// Preserve that attempt as failed rather than allowing an eternal
	// "running" row to become the default comparison target.
	_, _ = db.Exec(`UPDATE context_runs SET vm_status='failed', error=CASE WHEN error='' THEN 'assessment interrupted' ELSE error END, finished_at=? WHERE vm_status='running'`, time.Now().UTC().UnixMilli())
	_, _ = db.Exec(`UPDATE runs SET status='failed', finished_at=? WHERE status='running'`, time.Now().UTC().UnixMilli())
	_ = os.Chmod(path, 0o600)
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read history schema version: %w", err)
	}
	if version > 1 {
		return fmt.Errorf("history schema version %d is newer than this build understands", version)
	}
	if version == 1 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin history migration: %w", err)
	}
	stmts := []string{
		`CREATE TABLE runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source TEXT NOT NULL,
			started_at INTEGER NOT NULL,
			finished_at INTEGER,
			status TEXT NOT NULL,
			requested_contexts INTEGER NOT NULL DEFAULT 0,
			successful_contexts INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE context_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			endpoint TEXT NOT NULL,
			datacenter TEXT NOT NULL DEFAULT '',
			vcenter_id TEXT NOT NULL DEFAULT '',
			started_at INTEGER NOT NULL,
			finished_at INTEGER,
			vm_status TEXT NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			UNIQUE(run_id, name)
		)`,
		`CREATE TABLE vm_observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			context_run_id INTEGER NOT NULL REFERENCES context_runs(id) ON DELETE CASCADE,
			moref TEXT NOT NULL,
			instance_uuid TEXT NOT NULL DEFAULT '',
			bios_uuid TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			payload BLOB NOT NULL,
			UNIQUE(context_run_id, moref)
		)`,
		`CREATE TABLE snapshot_observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			vm_observation_id INTEGER NOT NULL REFERENCES vm_observations(id) ON DELETE CASCADE,
			moref TEXT NOT NULL,
			numeric_id INTEGER NOT NULL,
			parent_moref TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			create_time INTEGER NOT NULL,
			power_state TEXT NOT NULL,
			quiesced INTEGER NOT NULL,
			current_snapshot INTEGER NOT NULL
		)`,
		`CREATE INDEX vm_identity_instance ON vm_observations(instance_uuid)`,
		`CREATE INDEX vm_identity_bios ON vm_observations(bios_uuid)`,
		`CREATE INDEX snapshots_time ON snapshot_observations(create_time)`,
		`PRAGMA user_version = 1`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate history database: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit history migration: %w", err)
	}
	return nil
}

func millis(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UnixMilli()
}

func fromMillis(v sql.NullInt64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return time.UnixMilli(v.Int64).UTC()
}

func (s *Store) StartRun(ctx context.Context, source string, contexts []*config.Context, now time.Time) (Run, error) {
	if source == "" {
		source = "cli"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO runs(source,started_at,status,requested_contexts) VALUES(?,?,?,?)`, source, now.UnixMilli(), RunRunning, len(contexts))
	if err != nil {
		_ = tx.Rollback()
		return Run{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return Run{}, err
	}
	for _, cc := range contexts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO context_runs(run_id,name,endpoint,datacenter,started_at,vm_status) VALUES(?,?,?,?,?,?)`, id, cc.Name, cc.Endpoint, cc.Datacenter, now.UnixMilli(), "running"); err != nil {
			_ = tx.Rollback()
			return Run{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Run{}, err
	}
	return Run{ID: id, Source: source, StartedAt: now.UTC(), Status: RunRunning, RequestedContexts: len(contexts)}, nil
}

func (s *Store) SaveContext(ctx context.Context, runID int64, result ContextResult, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	status := result.Status
	if status == "" {
		status = "success"
	}
	_, err = tx.ExecContext(ctx, `UPDATE context_runs SET vcenter_id=?,finished_at=?,vm_status=?,error=? WHERE run_id=? AND name=?`, result.VCenterID, now.UnixMilli(), status, result.Error, runID, result.Name)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	var contextRunID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM context_runs WHERE run_id=? AND name=?`, runID, result.Name).Scan(&contextRunID); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, vm := range result.VMs {
		payload, err := json.Marshal(vm.VM)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO vm_observations(context_run_id,moref,instance_uuid,bios_uuid,name,payload) VALUES(?,?,?,?,?,?)`, contextRunID, vm.VM.ID, vm.VM.InstanceUUID, vm.VM.BIOSUUID, vm.VM.Name, payload)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		vmID, err := res.LastInsertId()
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		for _, snap := range vm.VM.Snapshots {
			_, err = tx.ExecContext(ctx, `INSERT INTO snapshot_observations(vm_observation_id,moref,numeric_id,parent_moref,name,description,create_time,power_state,quiesced,current_snapshot) VALUES(?,?,?,?,?,?,?,?,?,?)`, vmID, snap.ID, snap.NumericID, snap.ParentID, snap.Name, snap.Description, snap.CreateTime.UnixMilli(), snap.PowerState, boolInt(snap.Quiesced), boolInt(snap.Current))
			if err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit()
}

type ContextResult struct {
	Name      string
	VCenterID string
	Status    string
	Error     string
	VMs       []Observation
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *Store) FinishRun(ctx context.Context, runID int64, now time.Time) (Run, error) {
	var requested, success, failed int
	if err := s.db.QueryRowContext(ctx, `SELECT requested_contexts, successful_contexts FROM runs WHERE id=?`, runID).Scan(&requested, &success); err != nil {
		return Run{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM context_runs WHERE run_id=? AND vm_status IN ('success','empty')`, runID).Scan(&success); err != nil {
		return Run{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM context_runs WHERE run_id=? AND vm_status='running'`, runID).Scan(&failed); err != nil {
		return Run{}, err
	}
	status := RunPartial
	if success == 0 {
		status = RunFailed
	} else if success == requested && failed == 0 {
		status = RunComplete
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE runs SET finished_at=?,status=?,successful_contexts=? WHERE id=?`, now.UnixMilli(), status, success, runID); err != nil {
		return Run{}, err
	}
	return s.GetRun(ctx, runID)
}

func (s *Store) GetRun(ctx context.Context, id int64) (Run, error) {
	var r Run
	var start, finish sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,source,started_at,finished_at,status,requested_contexts,successful_contexts FROM runs WHERE id=?`, id).Scan(&r.ID, &r.Source, &start, &finish, &r.Status, &r.RequestedContexts, &r.SuccessfulContexts)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, fmt.Errorf("assessment %d not found", id)
	}
	if err != nil {
		return Run{}, err
	}
	r.StartedAt, r.FinishedAt = fromMillis(start), fromMillis(finish)
	return r, nil
}

func (s *Store) Runs(ctx context.Context) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,source,started_at,finished_at,status,requested_contexts,successful_contexts FROM runs ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		var start, finish sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Source, &start, &finish, &r.Status, &r.RequestedContexts, &r.SuccessfulContexts); err != nil {
			return nil, err
		}
		r.StartedAt, r.FinishedAt = fromMillis(start), fromMillis(finish)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRun(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM runs WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("assessment %d not found", id)
	}
	return nil
}

func (s *Store) SnapshotAges(ctx context.Context, runID int64, olderThan time.Duration) ([]SnapshotAge, error) {
	return s.snapshotAges(ctx, runID, olderThan)
}

func (s *Store) History(ctx context.Context, query, contextName string) ([]VMHistoryEntry, error) {
	runs, err := s.Runs(ctx)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	var out []VMHistoryEntry
	for _, run := range runs {
		byContext, contexts, err := s.loadVMs(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		for id, c := range contexts {
			if contextName != "" && c.Name != contextName {
				continue
			}
			for _, v := range byContext[id] {
				vm := v.observation.VM
				matches := needle == "" || strings.EqualFold(vm.Name, needle) || strings.EqualFold(vm.ID, needle) || strings.EqualFold(vm.InstanceUUID, needle) || strings.EqualFold(vm.BIOSUUID, needle)
				if matches {
					out = append(out, VMHistoryEntry{Run: run, Observation: v.observation, Snapshots: v.snapshots})
				}
			}
		}
	}
	return out, nil
}

type storedVM struct {
	observation Observation
	snapshots   []vsphere.VMSnapshot
	seen        time.Time
}

func (s *Store) loadVMs(ctx context.Context, runID int64) (map[int64][]storedVM, map[int64]ContextRun, error) {
	contexts := make(map[int64]ContextRun)
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,endpoint,datacenter,vcenter_id,started_at,finished_at,vm_status,error FROM context_runs WHERE run_id=?`, runID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var c ContextRun
		var start, finish sql.NullInt64
		if err := rows.Scan(&id, &c.Name, &c.Endpoint, &c.Datacenter, &c.VCenterID, &start, &finish, &c.VMStatus, &c.Error); err != nil {
			return nil, nil, err
		}
		c.RunID = runID
		c.StartedAt, c.FinishedAt = fromMillis(start), fromMillis(finish)
		contexts[id] = c
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	byContext := make(map[int64][]storedVM)
	for id := range contexts {
		vrows, err := s.db.QueryContext(ctx, `SELECT id,moref,instance_uuid,bios_uuid,payload FROM vm_observations WHERE context_run_id=?`, id)
		if err != nil {
			return nil, nil, err
		}
		for vrows.Next() {
			var vmID int64
			var moref, iu, bu string
			var payload []byte
			if err := vrows.Scan(&vmID, &moref, &iu, &bu, &payload); err != nil {
				vrows.Close()
				return nil, nil, err
			}
			var vm vsphere.VM
			if err := json.Unmarshal(payload, &vm); err != nil {
				vrows.Close()
				return nil, nil, err
			}
			vm.ID = moref
			vm.InstanceUUID = iu
			vm.BIOSUUID = bu
			snaps, err := s.loadSnapshots(ctx, vmID)
			if err != nil {
				vrows.Close()
				return nil, nil, err
			}
			byContext[id] = append(byContext[id], storedVM{observation: Observation{VCenterID: contexts[id].VCenterID, Context: contexts[id].Name, VM: vm}, snapshots: snaps, seen: contexts[id].FinishedAt})
		}
		vrows.Close()
	}
	return byContext, contexts, nil
}

func (s *Store) loadSnapshots(ctx context.Context, vmID int64) ([]vsphere.VMSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT moref,numeric_id,parent_moref,name,description,create_time,power_state,quiesced,current_snapshot FROM snapshot_observations WHERE vm_observation_id=?`, vmID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []vsphere.VMSnapshot
	for rows.Next() {
		var n int32
		var moref, parent, name, desc, power string
		var ct int64
		var q, cur int
		if err := rows.Scan(&moref, &n, &parent, &name, &desc, &ct, &power, &q, &cur); err != nil {
			return nil, err
		}
		out = append(out, vsphere.VMSnapshot{ID: moref, NumericID: n, ParentID: parent, Name: name, Description: desc, CreateTime: time.UnixMilli(ct).UTC(), PowerState: power, Quiesced: q != 0, Current: cur != 0})
	}
	return out, rows.Err()
}
