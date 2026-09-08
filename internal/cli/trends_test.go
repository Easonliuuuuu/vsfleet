package cli

import (
	"bytes"
	"context"
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
