package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func runAssessment(t *testing.T, dbPath string, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	a := &App{HistoryPath: dbPath, Out: &out, Err: &errOut}
	root := NewRootCommand(a)
	root.SetArgs(append([]string{"--history-db", dbPath, "assessment"}, args...))
	defer func() { _ = a.Close(context.Background()) }()
	err := root.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func TestTrendCommandsRejectUnknownContextWithExitCodeOne(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	for _, tc := range [][]string{
		{"trends", "churn", "--context", "ghost"},
		{"trends", "snapshots", "--context", "ghost"},
		{"trends", "capacity", "--context", "ghost"},
		{"capacity", "latest", "--context", "ghost"},
	} {
		_, _, err := runAssessment(t, dbPath, tc...)
		if err == nil || !strings.Contains(err.Error(), "unknown assessment context") {
			t.Fatalf("%v: err = %v, want unknown-context failure", tc, err)
		}
		// A plain error maps to exit code 1 (no ExitCode() override).
		var coded interface{ ExitCode() int }
		if errors.As(err, &coded) {
			t.Fatalf("%v: error carries exit code %d, want plain exit 1", tc, coded.ExitCode())
		}
	}
}

func TestTrendCommandScopedToKnownContextSucceeds(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	if _, _, err := runAssessment(t, dbPath, "trends", "capacity", "--context", "PROD", "-o", "json"); err != nil {
		t.Fatalf("known context (case-insensitive): %v", err)
	}
}

func TestStoredAssessmentCommandsRejectUnknownContext(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	for _, args := range [][]string{
		{"report", "--context", "ghost"},
		{"snapshots", "--context", "ghost"},
		{"findings", "--context", "ghost"},
		{"readiness", "--context", "ghost"},
		{"orphans", "--context", "ghost"},
	} {
		_, _, err := runAssessment(t, dbPath, args...)
		if err == nil || !strings.Contains(err.Error(), "unknown assessment context") {
			t.Fatalf("%v: err=%v, want unknown-context failure", args, err)
		}
	}
}

func TestAssessmentReportScopesStoredEvidence(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	stdout, _, err := runAssessment(t, dbPath, "report", "--context", " PROD ", "-o", "json")
	if err != nil {
		t.Fatalf("scoped report: %v", err)
	}
	var report struct {
		VMCount        int `json:"vm_count"`
		DatastoreCount int `json:"datastore_count"`
		Coverage       []struct {
			Context string `json:"context"`
		} `json:"coverage"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("report JSON: %v\n%s", err, stdout)
	}
	if report.VMCount != 0 || report.DatastoreCount != 1 {
		t.Fatalf("scoped report counts = vm %d datastore %d", report.VMCount, report.DatastoreCount)
	}
	for _, coverage := range report.Coverage {
		if !strings.EqualFold(coverage.Context, "prod") {
			t.Fatalf("unselected coverage leaked: %+v", coverage)
		}
	}
}

func TestAssessmentMetadataCommandsRejectContext(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	for _, args := range [][]string{
		{"list", "--context", "prod"},
		{"doctor", "--context", "prod"},
		{"prune", "--context", "prod"},
		{"backup", "/tmp/vsfleet-context-test.db", "--context", "prod"},
	} {
		_, _, err := runAssessment(t, dbPath, args...)
		if err == nil || !strings.Contains(err.Error(), "--context is not supported") {
			t.Fatalf("%v: err=%v, want unsupported-context failure", args, err)
		}
	}
}
