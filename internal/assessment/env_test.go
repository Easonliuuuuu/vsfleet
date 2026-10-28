package assessment

import (
	"strings"
	"testing"
)

// VSFLEET_HISTORY_DB is still read for one release: a container that kept
// the old name must keep its database rather than silently open a new one.
func TestDefaultPathReadsTheLegacyVariableWithAWarning(t *testing.T) {
	for _, tc := range []struct {
		name, db, legacy, wantPath, wantWarning string
	}{
		{"new variable", "/data/new.db", "", "/data/new.db", ""},
		{"legacy variable", "", "/data/old.db", "/data/old.db", "rename it to VSFLEET_DB"},
		{"both set", "/data/new.db", "/data/old.db", "/data/new.db", "VSFLEET_HISTORY_DB is ignored because VSFLEET_DB is set"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvDBPath, tc.db)
			t.Setenv(LegacyEnvDBPath, tc.legacy)
			got, err := DefaultPath()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.wantPath {
				t.Errorf("DefaultPath() = %q, want %q", got, tc.wantPath)
			}
			warning := LegacyEnvWarning()
			if tc.wantWarning == "" && warning != "" || !strings.Contains(warning, tc.wantWarning) {
				t.Errorf("LegacyEnvWarning() = %q, want %q", warning, tc.wantWarning)
			}
		})
	}
}
