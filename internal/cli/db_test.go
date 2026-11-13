package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyDBVariableDoesNotWarn(t *testing.T) {
	t.Setenv("VSFLEET_HISTORY_DB", filepath.Join(t.TempDir(), "old.db"))
	out, stderr, err := runArgs(t, "api-resources")
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want no legacy variable warning", stderr)
	}
	if !strings.Contains(out, "NAME") {
		t.Errorf("api-resources = %q, want the resource table", out)
	}
}
