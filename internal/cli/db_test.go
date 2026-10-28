package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
)

// The deprecated variable still works, and every command says so on stderr.
func TestLegacyDBVariableWarns(t *testing.T) {
	dbPath := seedHistory(t, time.Now().UTC().Truncate(time.Second), "", assessment.ContextResult{Name: "prod", VCenterID: "vc-1", Status: "success"})
	t.Setenv(assessment.EnvDBPath, "")
	t.Setenv(assessment.LegacyEnvDBPath, dbPath)
	var out, errOut bytes.Buffer
	a := &App{Out: &out, Err: &errOut}
	root := NewRootCommand(a)
	root.SetArgs([]string{"-o", "json", "capture", "list"})
	defer func() { _ = a.Close(context.Background()) }()
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut.String(), "warning: VSFLEET_HISTORY_DB is deprecated") {
		t.Errorf("stderr = %q, want the deprecation warning", errOut.String())
	}
	if !strings.Contains(out.String(), `"id": 1`) {
		t.Errorf("capture list = %s, want the capture in the legacy database", out.String())
	}
}
