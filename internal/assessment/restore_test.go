package assessment

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
)

func TestRestoreRequiresForceOnlyOverExistingHistory(t *testing.T) {
	tests := []struct {
		name     string
		existing bool
		force    bool
		wantErr  error
	}{
		{name: "fresh history without force", existing: false, force: false},
		{name: "fresh history with force", existing: false, force: true},
		{name: "existing history without force", existing: true, force: false, wantErr: ErrRestoreNeedsForce},
		{name: "existing history with force", existing: true, force: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			source := openRestoreStore(t, filepath.Join(dir, "source.db"))
			sourceRun := startRestoreRun(t, source, "source")
			backup := filepath.Join(dir, "backup.db")
			if err := source.Backup(ctx, backup, false); err != nil {
				t.Fatal(err)
			}

			target := openRestoreStore(t, filepath.Join(dir, "history.db"))
			var targetRun Run
			if tt.existing {
				targetRun = startRestoreRun(t, target, "target")
			}
			_, err := target.Restore(ctx, backup, tt.force)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Restore() error = %v, want %v", err, tt.wantErr)
				}
				got, err := target.GetRun(ctx, targetRun.ID)
				if err != nil || got.Source != "target" {
					t.Fatalf("existing run after refused restore = %+v, %v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Restore() error = %v", err)
			}
			got, err := target.GetRun(ctx, sourceRun.ID)
			if err != nil || got.Source != "source" {
				t.Fatalf("restored run = %+v, %v", got, err)
			}
		})
	}
}

func openRestoreStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func startRestoreRun(t *testing.T, s *Store, source string) Run {
	t.Helper()
	r, err := s.StartRun(context.Background(), source, []*config.Context{testContext("prod")}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return r
}
