package assessment

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// Issue #523: an import carries fewer fields than a live capture, so chaining
// it into a VM's history reported the difference as changes and undid them at
// the next capture. Real changes across the import must still be reported.
func TestTimelineLeavesImportsOutOfALiveHistory(t *testing.T) {
	live := testVM("billing", "vm-1", "instance-1", "bios-1", "esx-1")
	live.ConfigurationAvailable, live.Firmware, live.Folder = true, "bios", "/"
	live.NICs = []vsphere.VMNIC{{Key: 4000, Adapter: "Vmxnet3", MACAddress: "00:50:56:b9:02:2b"}}
	resized := live
	resized.MemoryMB = 8192
	// What the lab's workbook stored for such a VM: no firmware, NICs keyed
	// from 0 and the folder under its datacenter.
	imported := live
	imported.ConfigurationAvailable, imported.Firmware, imported.Folder = false, "", "/dc1"
	imported.NICs = []vsphere.VMNIC{{MACAddress: "00:50:56:b9:02:2b", Adapter: "Vmxnet3"}}

	type capture struct {
		source string
		vm     *vsphere.VM // nil when the capture read the vCenter without it
	}
	for _, tc := range []struct {
		name     string
		captures []capture
		want     []string
	}{
		{
			name:     "an import between two identical live captures",
			captures: []capture{{"cli", &live}, {SourceRVToolsImport, &imported}, {"cli", &live}},
			want:     []string{"first_seen"},
		},
		{
			name:     "a real change across an import",
			captures: []capture{{"cli", &live}, {SourceRVToolsImport, &imported}, {"cli", &resized}},
			want:     []string{"first_seen", "modified"},
		},
		{
			name:     "an import that does not hold the VM",
			captures: []capture{{"cli", &live}, {SourceRVToolsImport, nil}, {"cli", &live}},
			want:     []string{"first_seen"},
		},
		{
			name:     "a VM known only from imports",
			captures: []capture{{SourceRVToolsImport, &imported}, {SourceRVToolsImport, &imported}},
			want:     []string{"first_seen"},
		},
		{
			name:     "the VM goes between live captures",
			captures: []capture{{"cli", &live}, {SourceRVToolsImport, &imported}, {"cli", nil}},
			want:     []string{"first_seen", "vanished"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx := context.Background()
			when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			for i, c := range tc.captures {
				at := when.Add(time.Duration(i) * time.Hour)
				r, err := s.StartRun(ctx, c.source, []*config.Context{testContext("prod")}, at)
				if err != nil {
					t.Fatal(err)
				}
				result := ContextResult{Name: "prod", VCenterID: "vc-1", Status: "empty"}
				if c.vm != nil {
					result.Status = "success"
					result.VMs = []Observation{{VCenterID: "vc-1", Context: "prod", VM: *c.vm}}
				}
				if err := s.SaveContext(ctx, r.ID, result, at.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				if _, err := s.FinishRun(ctx, r.ID, at.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			events, err := s.Timeline(ctx, "instance-1", "", false, false)
			if err != nil {
				t.Fatal(err)
			}
			var kinds []string
			for _, e := range events {
				kinds = append(kinds, e.Kind)
			}
			if !slices.Equal(kinds, tc.want) {
				t.Fatalf("events = %v, want %v", kinds, tc.want)
			}
		})
	}
}
