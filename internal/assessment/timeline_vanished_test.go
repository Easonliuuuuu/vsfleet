package assessment

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
)

// TestTimelineVanishedFollowsTheVMsOwnVCenter checks that a VM missing from
// the next run is recorded as vanished whenever that run read its vCenter,
// even if another vCenter failed and left the run partial (issue #473).
func TestTimelineVanishedFollowsTheVMsOwnVCenter(t *testing.T) {
	for _, tc := range []struct {
		name         string
		prodStatus   string // the VM's own vCenter in the second run
		otherStatus  string // an unrelated vCenter in the second run
		wantVanished bool
	}{
		{"every vCenter read", "success", "success", true},
		{"another vCenter failed", "success", "failed", true},
		{"its own vCenter failed", "failed", "success", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx := context.Background()
			contexts := []*config.Context{testContext("prod"), testContext("dr")}
			when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			vm := testVM("billing", "vm-1", "instance-1", "bios-1", "esx-1")
			save := func(at time.Time, prodStatus, otherStatus string, prodVMs []Observation) {
				t.Helper()
				r, err := s.StartRun(ctx, "test", contexts, at)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.SaveContext(ctx, r.ID, ContextResult{Name: "prod", VCenterID: "vc-prod", Status: prodStatus, VMs: prodVMs}, at.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				if err := s.SaveContext(ctx, r.ID, ContextResult{Name: "dr", VCenterID: "vc-dr", Status: otherStatus}, at.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				if _, err := s.FinishRun(ctx, r.ID, at.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			save(when, "success", "success", []Observation{{VCenterID: "vc-prod", Context: "prod", VM: vm}})
			save(when.Add(time.Hour), tc.prodStatus, tc.otherStatus, nil)

			events, err := s.Timeline(ctx, "instance-1", "", false, false)
			if err != nil {
				t.Fatal(err)
			}
			vanished := false
			for _, e := range events {
				vanished = vanished || e.Kind == "vanished"
			}
			if vanished != tc.wantVanished {
				t.Fatalf("vanished=%v, want %v; events=%+v", vanished, tc.wantVanished, events)
			}
		})
	}
}

// TestTimelineScopedToOneContextIgnoresCapturesThroughOthers checks that a
// history scoped to some contexts judges a VM's absence only by the captures
// those contexts made. Two contexts can read the same vCenter; a capture
// through the other one is not a gap in this one's view (issue #522).
func TestTimelineScopedToOneContextIgnoresCapturesThroughOthers(t *testing.T) {
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	vm := testVM("billing", "vm-1", "instance-1", "bios-1", "esx-1")
	type capture struct {
		context, status string
	}
	for _, tc := range []struct {
		name     string
		runs     [][]capture // each run's contexts, oldest first; all read vc-1
		scope    string
		wantKind []string
	}{
		{
			name:     "a capture through another context sits between two of this one",
			runs:     [][]capture{{{"prod", "success"}}, {{"admin", "success"}}, {{"prod", "success"}}},
			scope:    "prod",
			wantKind: []string{"first_seen"},
		},
		{
			name:     "the mirrored scope",
			runs:     [][]capture{{{"admin", "success"}}, {{"prod", "success"}}, {{"admin", "success"}}},
			scope:    "admin",
			wantKind: []string{"first_seen"},
		},
		{
			name:     "this context failed while another read the same vCenter",
			runs:     [][]capture{{{"prod", "success"}}, {{"prod", "failed"}, {"admin", "success"}}},
			scope:    "prod",
			wantKind: []string{"first_seen"},
		},
		{
			name:     "this context read the vCenter and the VM was gone",
			runs:     [][]capture{{{"prod", "success"}}, {{"admin", "success"}}, {{"prod", "empty"}}},
			scope:    "prod",
			wantKind: []string{"first_seen", "vanished"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx := context.Background()
			for i, run := range tc.runs {
				at := when.Add(time.Duration(i) * time.Hour)
				var contexts []*config.Context
				for _, c := range run {
					contexts = append(contexts, testContext(c.context))
				}
				r, err := s.StartRun(ctx, "test", contexts, at)
				if err != nil {
					t.Fatal(err)
				}
				for _, c := range run {
					var vms []Observation
					if c.status == "success" {
						vms = []Observation{{VCenterID: "vc-1", Context: c.context, VM: vm}}
					}
					if err := s.SaveContext(ctx, r.ID, ContextResult{Name: c.context, VCenterID: "vc-1", Status: c.status, VMs: vms}, at.Add(time.Second)); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := s.FinishRun(ctx, r.ID, at.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			}

			events, err := s.Timeline(ctx, "instance-1", tc.scope, false, false)
			if err != nil {
				t.Fatal(err)
			}
			var kinds []string
			for _, e := range events {
				kinds = append(kinds, e.Kind)
			}
			if !slices.Equal(kinds, tc.wantKind) {
				t.Fatalf("events = %v, want %v", kinds, tc.wantKind)
			}
		})
	}
}
