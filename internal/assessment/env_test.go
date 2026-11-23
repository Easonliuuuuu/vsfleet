package assessment

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPathIgnoresLegacyVariable(t *testing.T) {
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	defaultPath := filepath.Join(dir, "vsfleet", "history.db")
	dbPath := filepath.Join(t.TempDir(), "new.db")
	legacyPath := filepath.Join(t.TempDir(), "old.db")
	for _, tc := range []struct {
		name, db, legacy, wantPath string
	}{
		{"default", "", "", defaultPath},
		{"new variable", dbPath, "", dbPath},
		{"legacy variable ignored", "", legacyPath, defaultPath},
		{"both set", dbPath, legacyPath, dbPath},
		{"blank new variable", " ", legacyPath, defaultPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvDBPath, tc.db)
			t.Setenv("VSFLEET_HISTORY_DB", tc.legacy)
			got, err := DefaultPath()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.wantPath {
				t.Errorf("DefaultPath() = %q, want %q", got, tc.wantPath)
			}
		})
	}
}
