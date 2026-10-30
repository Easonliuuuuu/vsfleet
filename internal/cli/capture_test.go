package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
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

// Issue #523: "capture diff" pairs an import with a live capture only when
// told to, or when nothing else can be compared, and warns when it does.
func TestCaptureDiffKeepsImportsApartFromLiveCaptures(t *testing.T) {
	const live, imported = "cli", assessment.SourceRVToolsImport
	for _, tc := range []struct {
		name        string
		sources     []string
		args        []string
		base, tgt   int64
		wantWarning bool
	}{
		{name: "latest is live, an import before it", sources: []string{live, imported, live}, base: 1, tgt: 3},
		{name: "latest is an import", sources: []string{imported, live, imported}, base: 1, tgt: 3},
		{name: "one live capture named", sources: []string{live, imported, live}, args: []string{"3"}, base: 1, tgt: 3},
		{name: "no capture of the same kind", sources: []string{live, imported}, base: 1, tgt: 2, wantWarning: true},
		{name: "both named", sources: []string{live, imported, live}, args: []string{"2", "3"}, base: 2, tgt: 3, wantWarning: true},
		{name: "live captures only", sources: []string{live, live, live}, base: 2, tgt: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "history.db")
			s, err := assessment.Open(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			for i, source := range tc.sources {
				at := when.Add(time.Duration(i) * time.Hour)
				r, err := s.StartRun(ctx, source, []*config.Context{{Name: "prod", Endpoint: "https://prod.example"}}, at)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.FinishRun(ctx, r.ID, at.Add(time.Minute)); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			out := mustRunCLI(t, dbPath, append([]string{"-o", "json", "capture", "diff"}, tc.args...)...)
			var d assessment.Diff
			if err := json.Unmarshal([]byte(out), &d); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if d.Base.ID != tc.base || d.Target.ID != tc.tgt {
				t.Errorf("compared %d and %d, want %d and %d", d.Base.ID, d.Target.ID, tc.base, tc.tgt)
			}
			warned := slices.ContainsFunc(d.Warnings, func(w string) bool { return strings.Contains(w, "RVTools import") })
			if warned != tc.wantWarning {
				t.Errorf("warnings = %q, want import warning %v", d.Warnings, tc.wantWarning)
			}
		})
	}
}
