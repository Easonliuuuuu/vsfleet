package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// Regression tests for issue #157: invalid or conflicting CLI input must fail
// early and explicitly rather than being silently reinterpreted, returning an
// empty-success result, or failing later during serialization.

// --- 1. --all-contexts must override --context for stored-evidence commands ---

func TestAllContextsOverridesUnknownContextForAssessmentCommands(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	for _, args := range [][]string{
		{"report", "--context", "missing", "--all-contexts"},
		{"snapshots", "--context", "missing", "--all-contexts"},
		{"findings", "--context", "missing", "--all-contexts"},
		{"readiness", "--context", "missing", "--all-contexts"},
		{"orphans", "--context", "missing", "--all-contexts"},
	} {
		_, _, err := runAssessment(t, dbPath, args...)
		if err != nil && strings.Contains(err.Error(), "unknown assessment context") {
			t.Fatalf("%v: err=%v, want --all-contexts to override unknown --context", args, err)
		}
	}
}

func TestAllContextsOverridesUnknownContextForTrendCommands(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	for _, tc := range [][]string{
		{"trends", "churn", "--context", "missing", "--all-contexts"},
		{"trends", "snapshots", "--context", "missing", "--all-contexts"},
		{"trends", "capacity", "--context", "missing", "--all-contexts"},
		{"capacity", "latest", "--context", "missing", "--all-contexts"},
	} {
		_, _, err := runAssessment(t, dbPath, tc...)
		if err != nil {
			t.Fatalf("%v: err=%v, want --all-contexts to override unknown --context", tc, err)
		}
	}
}

func TestAllContextsOverridesUnknownContextForTopologyCommands(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	for _, direction := range []string{"topology", "dependencies", "blast-radius"} {
		_, _, err := runTopology(t, dbPath, "--context", "missing", "--all-contexts", direction, "datastore", "datastore1", "-o", "json")
		if err != nil {
			t.Fatalf("%s: err=%v, want --all-contexts to override unknown --context", direction, err)
		}
	}
}

// TestAllContextsOverridesContextForAssessmentReport is the exact scenario
// from the issue: --context <unknown> alongside --all-contexts must not
// narrow or validate against the unknown name, and the broader selector must
// actually win rather than merely avoid an error.
func TestAllContextsOverridesContextForAssessmentReport(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	stdout, _, err := runAssessment(t, dbPath, "report", "--context", "missing", "--all-contexts", "-o", "json")
	if err != nil {
		t.Fatalf("--all-contexts should override unknown --context: %v", err)
	}
	var report struct {
		Coverage []struct {
			Context string `json:"context"`
		} `json:"coverage"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("report JSON: %v\n%s", err, stdout)
	}
	seen := map[string]bool{}
	for _, c := range report.Coverage {
		seen[strings.ToLower(c.Context)] = true
	}
	if !seen["prod"] || !seen["edge"] {
		t.Fatalf("expected --all-contexts to keep full stored scope, got %+v", report.Coverage)
	}
}

func TestAssessmentReportAllContextsAloneKeepsFullScope(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	stdout, _, err := runAssessment(t, dbPath, "report", "--all-contexts", "-o", "json")
	if err != nil {
		t.Fatalf("all-contexts report: %v", err)
	}
	var report struct {
		Coverage []struct {
			Context string `json:"context"`
		} `json:"coverage"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("report JSON: %v\n%s", err, stdout)
	}
	seen := map[string]bool{}
	for _, c := range report.Coverage {
		seen[strings.ToLower(c.Context)] = true
	}
	if !seen["prod"] || !seen["edge"] {
		t.Fatalf("expected both contexts in coverage, got %+v", report.Coverage)
	}
}

// --- 2. `assessment snapshots --at` must validate the assessment ID ---

func TestAssessmentSnapshotsValidatesAtID(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)

	if _, _, err := runAssessment(t, dbPath, "snapshots", "--at", "1", "-o", "json"); err != nil {
		t.Fatalf("existing --at: %v", err)
	}
	if _, _, err := runAssessment(t, dbPath, "snapshots", "-o", "json"); err != nil {
		t.Fatalf("default --at (latest): %v", err)
	}
	for _, tc := range []struct {
		name string
		at   string
	}{
		{"unknown positive ID", "999999"},
		{"negative ID", "-1"},
	} {
		_, _, err := runAssessment(t, dbPath, "snapshots", "--at", tc.at, "-o", "json")
		if err == nil {
			t.Fatalf("%s: unexpectedly succeeded", tc.name)
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("%s: err=%v, want an \"assessment ... not found\" failure", tc.name, err)
		}
	}
}

// --- 3. Percentage threshold flags must reject non-finite values ---

func TestHealthThresholdsRejectNonFiniteValues(t *testing.T) {
	dbPath := newHealthTestHistoryDB(t, false)
	for _, flag := range []string{"--min-datastore-free", "--min-guest-disk-free"} {
		for _, value := range []string{"NaN", "+Inf", "-Inf"} {
			_, _, err := runHealth(t, dbPath, flag, value, "-o", "json")
			if err == nil || !strings.Contains(err.Error(), "must be between 0 and 100") {
				t.Fatalf("%s=%s: err=%v, want finite-percentage validation failure", flag, value, err)
			}
		}
		for _, value := range []string{"0", "100"} {
			if _, _, err := runHealth(t, dbPath, flag, value, "-o", "json"); err != nil {
				t.Fatalf("%s=%s: %v", flag, value, err)
			}
		}
	}
}

func TestCapacityThresholdRejectsNonFiniteValues(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		_, _, err := runCapacity(t, dbPath, "latest", "--min-free", value, "-o", "json")
		if err == nil || !strings.Contains(err.Error(), "must be between 0 and 100") {
			t.Fatalf("--min-free=%s: err=%v, want finite-percentage validation failure", value, err)
		}
	}
	for _, value := range []string{"0", "100"} {
		if _, _, err := runCapacity(t, dbPath, "latest", "--min-free", value, "-o", "json"); err != nil {
			t.Fatalf("--min-free=%s: %v", value, err)
		}
	}
}

// --- 4. Topology --depth must be validated, not silently clamped ---

func TestTopologyDepthValidation(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	for _, direction := range []string{"dependencies", "blast-radius"} {
		for _, depth := range []string{"1", "5"} {
			if _, _, err := runTopology(t, dbPath, "--context", "prod", direction, "datastore", "datastore1", "--depth", depth, "-o", "json"); err != nil {
				t.Fatalf("%s --depth %s: %v", direction, depth, err)
			}
		}
		for _, depth := range []string{"0", "-1", "6", "100"} {
			_, _, err := runTopology(t, dbPath, "--context", "prod", direction, "datastore", "datastore1", "--depth", depth, "-o", "json")
			if err == nil || !strings.Contains(err.Error(), "--depth must be between 1 and 5") {
				t.Fatalf("%s --depth %s: err=%v, want depth-range failure", direction, depth, err)
			}
		}
	}
}
