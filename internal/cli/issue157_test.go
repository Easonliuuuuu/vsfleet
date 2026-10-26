package cli

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// Regression tests for issue #157: invalid or conflicting CLI input must fail
// early and explicitly rather than being silently reinterpreted, returning an
// empty-success result, or failing later during serialization.

// --- 1. --all-contexts must override --context for stored-evidence commands ---

// TestAllContextsOverridesContextForAssessmentReport is the exact scenario
// from the issue: --context <unknown> alongside --all-contexts must not
// narrow or validate against the unknown name, and the broader selector must
// actually win rather than merely avoid an error.
func TestAllContextsOverridesContextForAssessmentReport(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	stdout, _, err := runCLI(t, dbPath, "assessment", "report", "--context", "missing", "--all-contexts", "-o", "json")
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

// --- 2. `assessment snapshots --at` must validate the assessment ID ---

func TestAssessmentSnapshotsValidatesAtID(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)

	if _, _, err := runCLI(t, dbPath, "analyze", "snapshots", "--run", "1", "-o", "json"); err != nil {
		t.Fatalf("existing --at: %v", err)
	}
	if _, _, err := runCLI(t, dbPath, "analyze", "snapshots", "-o", "json"); err != nil {
		t.Fatalf("default --at (latest): %v", err)
	}
	for _, tc := range []struct {
		name string
		at   string
	}{
		{"unknown positive ID", "999999"},
	} {
		_, _, err := runCLI(t, dbPath, "analyze", "snapshots", "--run", tc.at, "-o", "json")
		if err == nil {
			t.Fatalf("%s: unexpectedly succeeded", tc.name)
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("%s: err=%v, want an \"assessment ... not found\" failure", tc.name, err)
		}
	}
}

// --- 3. Percentage threshold flags must reject non-finite values ---

func TestValidatePercent(t *testing.T) {
	for _, tc := range []struct {
		value float64
		ok    bool
	}{
		{math.NaN(), false},
		{math.Inf(1), false},
		{-0.1, false},
		{100.1, false},
		{0, true},
		{100, true},
	} {
		if err := validatePercent("--min-free", tc.value); (err == nil) != tc.ok {
			t.Errorf("validatePercent(%v) = %v, want ok=%v", tc.value, err, tc.ok)
		}
	}
}

func TestPercentFlagsAreValidated(t *testing.T) {
	healthDB, capacityDB := newHealthTestHistoryDB(t, false), newTopologyTestHistoryDB(t)
	for _, args := range [][]string{
		{healthDB, "analyze", "health", "--min-datastore-free"},
		{healthDB, "analyze", "health", "--min-guest-disk-free"},
		{capacityDB, "analyze", "capacity", "--run", "latest", "--min-free"},
	} {
		_, _, err := runCLI(t, args[0], append(args[1:], "NaN")...)
		if err == nil || !strings.Contains(err.Error(), "must be between 0 and 100") {
			t.Errorf("%v NaN: err = %v, want the percentage validation failure", args[1:], err)
		}
	}
}

// --- 4. Topology --depth must be validated, not silently clamped ---

func TestTopologyDepthValidation(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	for _, direction := range []string{"deps", "blast-radius"} {
		for _, depth := range []string{"1", "5"} {
			if _, _, err := runCLI(t, dbPath, "--context", "prod", "analyze", direction, "datastore", "datastore1", "--depth", depth, "-o", "json"); err != nil {
				t.Fatalf("%s --depth %s: %v", direction, depth, err)
			}
		}
		for _, depth := range []string{"0", "6"} {
			_, _, err := runCLI(t, dbPath, "--context", "prod", "analyze", direction, "datastore", "datastore1", "--depth", depth, "-o", "json")
			if err == nil || !strings.Contains(err.Error(), "--depth must be between 1 and 5") {
				t.Fatalf("%s --depth %s: err=%v, want depth-range failure", direction, depth, err)
			}
		}
	}
}
