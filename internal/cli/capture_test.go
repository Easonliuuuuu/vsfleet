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

// seedPerContextCaptures stores one capture per context, in order, so IDs count
// from 1. Each context keeps its own vCenter.
func seedPerContextCaptures(t *testing.T, contexts ...string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "history.db")
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, name := range contexts {
		seedRun(t, dbPath, when.Add(time.Duration(i)*time.Hour), "", assessment.ContextResult{Name: name, VCenterID: "vc-" + name, Status: "success"})
	}
	return dbPath
}

// Issue #540: -n scopes latest, previous and a date in "describe capture".
func TestDescribeCaptureScopesSelectorsToContexts(t *testing.T) {
	dbPath := seedPerContextCaptures(t, "a", "b", "a", "b")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "latest", args: []string{"describe", "capture", "latest", "-n", "a"}, want: "Capture 3 "},
		{name: "previous", args: []string{"describe", "capture", "previous", "-n", "b"}, want: "Capture 2 "},
		{name: "date", args: []string{"describe", "capture", "2026-01-01T02:30:00Z", "-n", "b"}, want: "Capture 2 "},
		{name: "unscoped", args: []string{"describe", "capture", "latest"}, want: "Capture 4 "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := mustRunCLI(t, dbPath, tc.args...); !strings.HasPrefix(out, tc.want) {
				t.Fatalf("%v:\n%s\nwant it to start %q", tc.args, out, tc.want)
			}
		})
	}
}

// Issue #540: "capture diff" compares captures of one context, so a pair with
// nothing in common cannot read as no changes.
func TestCaptureDiffPairsCapturesThatShareAContext(t *testing.T) {
	for _, tc := range []struct {
		name      string
		contexts  []string
		args      []string
		base, tgt int64
	}{
		{name: "scoped default", contexts: []string{"a", "b", "a", "b"}, args: []string{"-n", "a"}, base: 1, tgt: 3},
		{name: "scoped selectors", contexts: []string{"a", "b", "a", "b"}, args: []string{"previous", "latest", "-n", "a"}, base: 1, tgt: 3},
		{name: "scoped date", contexts: []string{"a", "b", "a", "b"}, args: []string{"2026-01-01T02:30:00Z", "latest", "-n", "b"}, base: 2, tgt: 4},
		{name: "one selector scoped", contexts: []string{"a", "b", "a", "b"}, args: []string{"latest", "-n", "a"}, base: 1, tgt: 3},
		{name: "default skips another context", contexts: []string{"a", "b", "c", "a"}, base: 1, tgt: 4},
		{name: "default passes over a capture with no partner", contexts: []string{"a", "a", "b"}, base: 1, tgt: 2},
		{name: "one named skips another context", contexts: []string{"a", "b", "a"}, args: []string{"3"}, base: 1, tgt: 3},
		{name: "two named compare regardless", contexts: []string{"a", "b"}, args: []string{"1", "2"}, base: 1, tgt: 2},
		{name: "ID names its capture outside the scope", contexts: []string{"a", "b", "a"}, args: []string{"2", "3", "-n", "a"}, base: 2, tgt: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := seedPerContextCaptures(t, tc.contexts...)
			out := mustRunCLI(t, dbPath, append([]string{"-o", "json", "capture", "diff"}, tc.args...)...)
			var d assessment.Diff
			if err := json.Unmarshal([]byte(out), &d); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if d.Base.ID != tc.base || d.Target.ID != tc.tgt {
				t.Fatalf("compared %d and %d, want %d and %d", d.Base.ID, d.Target.ID, tc.base, tc.tgt)
			}
		})
	}
}

func TestCaptureDiffRefusesAPairThatSharesNoContext(t *testing.T) {
	for _, tc := range []struct {
		name     string
		contexts []string
		args     []string
		want     string
	}{
		{name: "default", contexts: []string{"a", "b"}, want: "no two captures share a context"},
		{name: "one named", contexts: []string{"a", "b"}, args: []string{"2"}, want: "no capture before 2 shares a context"},
		{name: "one capture", contexts: []string{"a"}, want: "need at least two captures to compare"},
		{name: "scoped to one capture", contexts: []string{"a", "b"}, args: []string{"-n", "a"}, want: "need at least two captures of a to compare"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := seedPerContextCaptures(t, tc.contexts...)
			if out, _, err := runCLI(t, dbPath, append([]string{"capture", "diff"}, tc.args...)...); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q; output:\n%s", err, tc.want, out)
			}
		})
	}
}

// The diff compares by vCenter, so renaming the context between two captures
// leaves them comparable.
func TestCaptureDiffPairsARenamedContext(t *testing.T) {
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dbPath := seedHistory(t, when, "", assessment.ContextResult{Name: "old", VCenterID: "vc-1", Status: "success"})
	seedRun(t, dbPath, when.Add(time.Hour), "", assessment.ContextResult{Name: "new", VCenterID: "vc-1", Status: "success"})
	var d assessment.Diff
	out := mustRunCLI(t, dbPath, "-o", "json", "capture", "diff")
	if err := json.Unmarshal([]byte(out), &d); err != nil || d.Base.ID != 1 || d.Target.ID != 2 {
		t.Fatalf("capture diff = %s (err %v), want 1 → 2", out, err)
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
