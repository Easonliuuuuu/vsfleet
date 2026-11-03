package assessment

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// saveSharedVCenterRun stores one capture in which every named context
// reached vCenter vc-1 and recorded the same VMs.
func saveSharedVCenterRun(t *testing.T, s *Store, when time.Time, contexts []string, vms ...vsphere.VM) int64 {
	t.Helper()
	ctx := context.Background()
	configs := make([]*config.Context, 0, len(contexts))
	for _, name := range contexts {
		configs = append(configs, testContext(name))
	}
	r, err := s.StartRun(ctx, "test", configs, when)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range contexts {
		var observations []Observation
		for _, vm := range vms {
			observations = append(observations, Observation{VCenterID: "vc-1", Context: name, VM: vm})
		}
		result := ContextResult{Name: name, VCenterID: "vc-1", Status: "success", VMs: observations, Collections: []CollectionResult{{Kind: "snapshot", Status: "success"}}}
		if err := s.SaveContext(ctx, r.ID, result, when.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.FinishRun(ctx, r.ID, when.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	return r.ID
}

func TestDiffComparesAVMOncePerVCenterAcrossContexts(t *testing.T) {
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	vm := testVM("billing", "vm-1", "instance-1", "bios-1", "esx-1")
	vm.Snapshots = []vsphere.VMSnapshot{{ID: "snapshot-1", Name: "before-upgrade", CreateTime: when.Add(-time.Hour)}}
	resized := vm
	resized.CPU = 4
	for _, tc := range []struct {
		name             string
		base, target     []string
		after            vsphere.VM
		wantModified     int
		wantOnlyRecorded []string
	}{
		{"every context against one", []string{"prod", "audit", "nohost"}, []string{"prod"}, vm, 0, []string{"audit", "nohost"}},
		{"one context against every context", []string{"prod"}, []string{"prod", "audit"}, vm, 0, []string{"audit"}},
		{"every context on both sides", []string{"prod", "audit"}, []string{"prod", "audit"}, resized, 1, nil},
		{"renamed context", []string{"prod"}, []string{"production"}, vm, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			baseID := saveSharedVCenterRun(t, s, when, tc.base, vm)
			targetID := saveSharedVCenterRun(t, s, when.Add(time.Hour), tc.target, tc.after)
			d, err := s.Diff(context.Background(), baseID, targetID, false)
			if err != nil {
				t.Fatal(err)
			}
			want := DiffCounts{Modified: tc.wantModified}
			if d.Counts != want {
				t.Fatalf("counts = %+v, want %+v; VMs %+v", d.Counts, want, d.VMs)
			}
			var only []string
			for _, c := range d.Coverage {
				if strings.Contains(c.Message, "was recorded only in") {
					only = append(only, c.Context)
				}
			}
			if strings.Join(sortedCopy(only), ",") != strings.Join(sortedCopy(tc.wantOnlyRecorded), ",") {
				t.Fatalf("one-sided contexts warned = %v, want %v; coverage %+v", only, tc.wantOnlyRecorded, d.Coverage)
			}
		})
	}
}
