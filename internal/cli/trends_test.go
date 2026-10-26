package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
)

func TestAssessmentReportScopesStoredEvidence(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	stdout, _, err := runCLI(t, dbPath, "describe", "capture", "latest", "--context", " PROD ", "-o", "json")
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
		{"capture", "list", "--context", "prod"},
		{"assessment", "doctor", "--context", "prod"},
		{"capture", "prune", "--context", "prod"},
		{"assessment", "backup", "/tmp/vsfleet-context-test.db", "--context", "prod"},
	} {
		_, _, err := runCLI(t, dbPath, args...)
		if err == nil || !strings.Contains(err.Error(), "--context is not supported") {
			t.Fatalf("%v: err=%v, want unsupported-context failure", args, err)
		}
	}
}

// Every command that reads stored evidence must refuse a --context the run
// never captured, with a plain error (exit 1), and --all-contexts must win
// over it rather than validate it (issue #157).
func TestStoredEvidenceCommandsScopeByContext(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	for _, args := range [][]string{
		{"describe", "capture", "latest"},
		{"analyze", "snapshots"},
		{"analyze", "health"},
		{"analyze", "readiness"},
		{"analyze", "orphans"},
		{"analyze", "churn"},
		{"analyze", "snapshots", "--trend"},
		{"analyze", "capacity", "--trend"},
		{"analyze", "capacity", "--run", "latest"},
		{"analyze", "topology", "datastore", "datastore1"},
		{"analyze", "deps", "datastore", "datastore1"},
		{"analyze", "blast-radius", "datastore", "datastore1"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, _, err := runCLI(t, dbPath, append(args, "--context", "ghost", "-o", "json")...)
			if !errors.Is(err, assessment.ErrUnknownStoredContext) {
				t.Fatalf("--context ghost: err = %v, want the unknown-context failure", err)
			}
			var coded interface{ ExitCode() int }
			if errors.As(err, &coded) {
				t.Fatalf("--context ghost: error carries exit code %d, want plain exit 1", coded.ExitCode())
			}
			if _, _, err := runCLI(t, dbPath, append(args, "--context", "ghost", "--all-contexts", "-o", "json")...); err != nil {
				t.Fatalf("--all-contexts did not override --context ghost: %v", err)
			}
		})
	}
}
