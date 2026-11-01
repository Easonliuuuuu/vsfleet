package assessment

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestRunMetadataSelectorsAndPinning(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := s.StartRunWithMetadata(context.Background(), "test", []*config.Context{testContext("prod")}, now, RunMetadata{Label: "nightly-1", Note: "baseline", Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun(context.Background(), r.ID)
	if err != nil || got.Label != "nightly-1" || !got.Pinned {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	if id, err := s.ResolveRun(context.Background(), "NIGHTLY-1"); err != nil || id != r.ID {
		t.Fatalf("resolve id=%d err=%v", id, err)
	}
	if err := s.DeleteRunSelector(context.Background(), "nightly-1"); err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("delete err=%v", err)
	}
	falseValue := false
	if _, err := s.UpdateRunFields(context.Background(), "nightly-1", nil, nil, &falseValue); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRunSelector(context.Background(), "nightly-1"); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRunByDate(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	start := func(at time.Time, label string) int64 {
		t.Helper()
		r, err := s.StartRunWithMetadata(ctx, "test", []*config.Context{testContext("prod")}, at, RunMetadata{Label: label})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.FinishRun(ctx, r.ID, at.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	march := start(time.Date(2026, 3, 31, 18, 0, 0, 0, time.Local), "")
	april := start(time.Date(2026, 4, 2, 9, 30, 0, 0, time.Local), "2026-04-05")

	for _, tc := range []struct {
		selector string
		want     int64
		wantErr  string
	}{
		{selector: "2026-03-31", want: march},
		{selector: "2026-04-01", want: march},
		{selector: "2026-04-02", want: april},
		{selector: "2026-04-02T09:29", want: march},
		{selector: "2026-04-02T09:30", want: april},
		{selector: time.Date(2026, 4, 2, 9, 30, 0, 0, time.Local).Format(time.RFC3339), want: april},
		// A label that reads as a date is the label, not the date.
		{selector: "2026-04-05", want: april},
		{selector: "2026-03-30", wantErr: "no capture was started by 2026-03-30"},
		{selector: "nightly", wantErr: `"nightly" not found; give an ID, label, date, latest or previous`},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			id, err := s.ResolveRun(ctx, tc.selector)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ResolveRun(%q) = %d, %v; want error %q", tc.selector, id, err, tc.wantErr)
				}
				return
			}
			if err != nil || id != tc.want {
				t.Fatalf("ResolveRun(%q) = %d, %v; want %d", tc.selector, id, err, tc.want)
			}
		})
	}
}

// TestResolveRunForContextsSkipsOtherContexts checks that latest, previous
// and a date pick among the captures of the selected contexts, while an ID
// still names its capture (issue #525).
func TestResolveRunForContextsSkipsOtherContexts(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	start := func(at time.Time, names ...string) int64 {
		t.Helper()
		var contexts []*config.Context
		for _, name := range names {
			contexts = append(contexts, testContext(name))
		}
		r, err := s.StartRun(ctx, "test", contexts, at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.FinishRun(ctx, r.ID, at.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	day := func(d int) time.Time { return time.Date(2026, 4, d, 9, 0, 0, 0, time.Local) }
	prodOld := start(day(1), "prod")
	prodNew := start(day(2), "prod")
	dr := start(day(3), "dr")
	both := start(day(4), "prod", "dr")
	lab := start(day(5), "lab")

	for _, tc := range []struct {
		selector string
		contexts []string
		want     int64
		wantErr  string
	}{
		{selector: "latest", want: lab},
		{selector: "latest", contexts: []string{"Prod"}, want: both},
		{selector: "previous", contexts: []string{"prod"}, want: prodNew},
		{selector: "previous", contexts: []string{"dr"}, want: dr},
		{selector: "latest", contexts: []string{"prod", "dr"}, want: both},
		{selector: "previous", contexts: []string{"prod", "dr"}, wantErr: "no previous capture of prod, dr"},
		{selector: "2026-04-03", contexts: []string{"prod"}, want: prodNew},
		{selector: "2026-04-01", contexts: []string{"dr"}, wantErr: "no capture of dr was started by 2026-04-01"},
		// An ID names its capture whatever the contexts; reading it checks them.
		{selector: strconv.FormatInt(prodOld, 10), contexts: []string{"dr"}, want: prodOld},
	} {
		t.Run(tc.selector+" "+strings.Join(tc.contexts, ","), func(t *testing.T) {
			id, err := s.ResolveRunForContexts(ctx, tc.selector, tc.contexts)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ResolveRunForContexts(%q, %v) = %d, %v; want error %q", tc.selector, tc.contexts, id, err, tc.wantErr)
				}
				return
			}
			if err != nil || id != tc.want {
				t.Fatalf("ResolveRunForContexts(%q, %v) = %d, %v; want %d", tc.selector, tc.contexts, id, err, tc.want)
			}
		})
	}

	// A context no capture recorded stays the distinct unknown-context error
	// the TUI explains as "not captured yet".
	if _, err := s.ResolveRunForContexts(ctx, "latest", []string{"ghost"}); !errors.Is(err, ErrUnknownStoredContext) {
		t.Fatalf("latest of ghost: err = %v, want ErrUnknownStoredContext", err)
	}
}

func TestCaptureLeaseFencesConcurrentWriters(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	lease, err := s.AcquireCaptureLease(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcquireCaptureLease(context.Background(), now); err == nil {
		t.Fatal("second capture acquired the lease")
	}
	if err := s.ReleaseCaptureLease(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcquireCaptureLease(context.Background(), now); err != nil {
		t.Fatal(err)
	}
}

func TestTimelineReportsRenameAndSnapshotEvents(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	v1 := testVM("billing", "vm-1", "instance-1", "bios-1", "esx-1")
	saveTestRun(t, s, base, v1)
	v2 := testVM("billing-renamed", "vm-1", "instance-1", "bios-1", "esx-1")
	v2.Snapshots = []vsphere.VMSnapshot{{ID: "snap-1", Name: "old", CreateTime: base.Add(-time.Hour)}}
	saveTestRun(t, s, base.Add(time.Hour), v2)
	events, err := s.Timeline(context.Background(), "instance-1", "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	var renamed, created bool
	for _, e := range events {
		renamed = renamed || e.Kind == "renamed"
		created = created || e.Kind == "snapshot-created"
	}
	if !renamed || !created {
		t.Fatalf("events=%+v", events)
	}
}

func TestTimelineFollowsCrossVCenterMoveByInstanceUUID(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cc := []*config.Context{testContext("prod")}
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, vc := range []string{"vc-a", "vc-b"} {
		r, err := s.StartRun(context.Background(), "test", cc, when.Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		v := testVM("billing", fmt.Sprintf("vm-%d", i+1), "instance-move", "bios-move", "esx")
		if err := s.SaveContext(context.Background(), r.ID, ContextResult{Name: "prod", VCenterID: vc, Status: "success", VMs: []Observation{{VCenterID: vc, Context: "prod", VM: v}}}, when.Add(time.Duration(i)*time.Hour+time.Minute)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.FinishRun(context.Background(), r.ID, when.Add(time.Duration(i)*time.Hour+time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.Timeline(context.Background(), "instance-move", "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Kind == "moved" {
			return
		}
	}
	t.Fatalf("events=%+v", events)
}

func TestTimelineRejectsDuplicateUUIDInOneRun(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := s.StartRun(context.Background(), "test", []*config.Context{testContext("prod")}, when)
	if err != nil {
		t.Fatal(err)
	}
	v1 := testVM("one", "vm-1", "duplicate", "bios-1", "esx")
	v2 := testVM("two", "vm-2", "duplicate", "bios-2", "esx")
	if err := s.SaveContext(context.Background(), r.ID, ContextResult{Name: "prod", VCenterID: "vc-a", Status: "success", VMs: []Observation{{VCenterID: "vc-a", Context: "prod", VM: v1}, {VCenterID: "vc-a", Context: "prod", VM: v2}}}, when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishRun(context.Background(), r.ID, when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, err = s.Timeline(context.Background(), "duplicate", "", false, false)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("err=%v", err)
	}
}

func TestTimelineDoesNotDecodeUnrelatedVMs(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := s.StartRun(context.Background(), "test", []*config.Context{testContext("prod")}, when)
	if err != nil {
		t.Fatal(err)
	}
	target := testVM("target", "vm-1", "instance-1", "bios-1", "esx")
	noise := testVM("noise", "vm-2", "instance-2", "bios-2", "esx")
	if err := s.SaveContext(context.Background(), r.ID, ContextResult{
		Name: "prod", VCenterID: "vc-a", Status: "success",
		VMs: []Observation{
			{VCenterID: "vc-a", Context: "prod", VM: target},
			{VCenterID: "vc-a", Context: "prod", VM: noise},
		},
	}, when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishRun(context.Background(), r.ID, when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE vm_observations SET payload='not-json' WHERE name='noise'`); err != nil {
		t.Fatal(err)
	}

	events, err := s.Timeline(context.Background(), "target", "", false, false)
	if err != nil {
		t.Fatalf("Timeline decoded an unrelated VM: %v", err)
	}
	if len(events) != 1 || events[0].Kind != "first_seen" || events[0].Name != "target" {
		t.Fatalf("events=%+v", events)
	}
}

func TestEvaluatePolicy(t *testing.T) {
	for _, tc := range []struct {
		rule string
		diff Diff
	}{
		{"appeared", Diff{Counts: DiffCounts{Appeared: 1}}},
		{"host-appeared", Diff{Resources: []ResourceChange{{Kind: "host", Changes: []string{"appeared"}}}}},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			p := EvaluatePolicy(tc.diff, PolicyOptions{FailOn: []string{tc.rule}})
			if p.Passed || len(p.Violations) != 1 || p.Violations[0].Rule != tc.rule {
				t.Fatalf("policy=%+v", p)
			}
		})
	}
}
