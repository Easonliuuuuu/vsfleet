package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/perf"
)

// seedTwoCaptures stores captures on 2026-01-01 and 2026-01-03 and labels the
// second one nightly.
func seedTwoCaptures(t *testing.T) string {
	t.Helper()
	prod := assessment.ContextResult{Name: "prod", VCenterID: "vc-1", Status: "success"}
	dbPath := seedHistory(t, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), "", prod)
	seedRun(t, dbPath, time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC), "", prod)
	if _, _, err := runCLI(t, dbPath, "capture", "update", "latest", "--label", "nightly"); err != nil {
		t.Fatalf("label capture: %v", err)
	}
	return dbPath
}

func TestCaptureListIsGetCaptures(t *testing.T) {
	dbPath := seedTwoCaptures(t)
	list := mustRunCLI(t, dbPath, "-o", "json", "capture", "list")
	if got := mustRunCLI(t, dbPath, "-o", "json", "get", "captures"); got != list {
		t.Fatalf("get captures and capture list differ:\n%s\n%s", got, list)
	}
	var runs []assessment.Run
	if err := json.Unmarshal([]byte(list), &runs); err != nil || len(runs) != 2 || runs[0].Label != "nightly" {
		t.Fatalf("capture list -o json = %s (err %v), want both captures, newest first", list, err)
	}
	if got := mustRunCLI(t, dbPath, "-o", "name", "get", "capture", "nightly"); got != "capture/2\n" {
		t.Fatalf("get capture nightly -o name = %q, want capture/2", got)
	}
	// A capture is not inside a capture, so --run cannot narrow it.
	if _, _, err := runCLI(t, dbPath, "get", "captures", "--run", "latest"); err == nil || !strings.Contains(err.Error(), "--run does not apply") {
		t.Fatalf("get captures --run: err = %v, want a refusal", err)
	}
}

func TestDescribeCapture(t *testing.T) {
	dbPath := seedTwoCaptures(t)
	out := mustRunCLI(t, dbPath, "describe", "capture", "nightly")
	if !strings.Contains(out, "Capture 2") || !strings.Contains(out, "Label: nightly") {
		t.Fatalf("describe capture nightly:\n%s", out)
	}
	if _, _, err := runCLI(t, dbPath, "describe", "capture", "latest", "--run", "previous"); err == nil || !strings.Contains(err.Error(), "--run does not apply") {
		t.Fatalf("describe capture --run: err = %v, want a refusal", err)
	}
	if _, _, err := runCLI(t, dbPath, "describe", "capture", "ghost"); err == nil || !strings.Contains(err.Error(), `capture "ghost" not found`) {
		t.Fatalf("describe capture ghost: err = %v, want not found", err)
	}
}

// capture diff selects captures the way --run does, dates included.
func TestCaptureDiffTakesADate(t *testing.T) {
	dbPath := seedTwoCaptures(t)
	var d struct {
		Base   assessment.Run `json:"base"`
		Target assessment.Run `json:"target"`
	}
	out := mustRunCLI(t, dbPath, "-o", "json", "capture", "diff", "2026-01-01", "nightly")
	if err := json.Unmarshal([]byte(out), &d); err != nil || d.Base.ID != 1 || d.Target.ID != 2 {
		t.Fatalf("capture diff 2026-01-01 nightly = %s (err %v), want 1 → 2", out, err)
	}
}

func TestGetPerfCollections(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	seedPerformance(t, dbPath)
	var windows []perf.Window
	out := mustRunCLI(t, dbPath, "-o", "json", "get", "perfcollections")
	if err := json.Unmarshal([]byte(out), &windows); err != nil || len(windows) != 1 || windows[0].Context != "prod" {
		t.Fatalf("get perfcollections = %s (err %v), want prod's one collection", out, err)
	}
	// -n narrows collections to their context, as it narrows stored analyses.
	if out := mustRunCLI(t, dbPath, "-o", "json", "-n", "edge", "get", "perfcollections"); strings.TrimSpace(out) != "[]" {
		t.Fatalf("get perfcollections -n edge = %s, want none", out)
	}
	// The seeded window spans a day, which the table reads as 1d, not 24h0m0s.
	if out := mustRunCLI(t, dbPath, "get", "perfcollections"); !strings.Contains(out, " 1d ") {
		t.Fatalf("get perfcollections table = %q, want a 1d span", out)
	}
}
