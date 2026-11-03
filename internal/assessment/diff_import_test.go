package assessment

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// Issue #546: an RVTools import leaves the fields its workbook does not carry
// empty and records NICs without a device key. A diff against a live capture
// must compare only what both sides recorded.
func TestDiffComparesAnImportOnlyOnFieldsItRecorded(t *testing.T) {
	live := testVM("billing", "vm-1", "instance-1", "bios-1", "esx-1")
	live.ConfigurationAvailable, live.Firmware, live.GuestID = true, "bios", "other5xLinux64Guest"
	live.Disks = []vsphere.VMDisk{{Key: 2000, CapacityBytes: 10 << 30, BackingType: "flatVer2"}}
	live.NICs = []vsphere.VMNIC{
		{Key: 4000, Label: "Network adapter 1", Adapter: "Vmxnet3", MACAddress: "00:50:56:b9:7a:0a", MACAddressType: "assigned"},
		{Key: 4001, Label: "Network adapter 2", Adapter: "Vmxnet3", MACAddress: "00:50:56:b9:7a:0b", MACAddressType: "assigned"},
	}
	liveHost := vsphere.Host{Location: vsphere.Location{Datacenter: "dc1", Path: "/dc1/host/esx-1"}, ID: "host-1", Name: "esx-1", Version: "8.0.3"}
	// What the lab's workbook stores for the same VM and host.
	imported := live
	imported.ConfigurationAvailable, imported.Firmware, imported.GuestID = false, "", ""
	imported.Disks = []vsphere.VMDisk{{Key: 2000, CapacityBytes: 10 << 30}}
	imported.NICs = []vsphere.VMNIC{
		{Label: "Network adapter 2", Adapter: "Vmxnet3", MACAddress: "00:50:56:B9:7A:0B"},
		{Label: "Network adapter 1", Adapter: "Vmxnet3", MACAddress: "00:50:56:b9:7a:0a"},
	}
	importedHost := liveHost
	importedHost.Location = vsphere.Location{}
	resized := imported
	resized.MemoryMB = 8192
	remac := imported
	remac.NICs = []vsphere.VMNIC{{Label: "Network adapter 1", Adapter: "Vmxnet3", MACAddress: "00:50:56:b9:00:01"}, imported.NICs[0]}
	firmwareLost := live
	firmwareLost.Firmware = ""

	type side struct {
		source string
		vm     vsphere.VM
		host   vsphere.Host
	}
	for _, tc := range []struct {
		name         string
		base, target side
		want         []string
		wantLeftOut  bool
	}{
		{"live then import", side{"cli", live, liveHost}, side{SourceRVToolsImport, imported, importedHost}, nil, true},
		{"import then live", side{SourceRVToolsImport, imported, importedHost}, side{"cli", live, liveHost}, nil, true},
		{"a real change across the import", side{"cli", live, liveHost}, side{SourceRVToolsImport, resized, importedHost}, []string{"memory"}, true},
		{"a NIC's MAC changed, matched by label", side{"cli", live, liveHost}, side{SourceRVToolsImport, remac, importedHost}, []string{"migration_configuration.nics[4000].mac_address"}, true},
		{"two live captures still compare every field", side{"cli", live, liveHost}, side{"cli", firmwareLost, liveHost}, []string{"migration_configuration.firmware"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx := context.Background()
			when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			var ids [2]int64
			for i, c := range []side{tc.base, tc.target} {
				at := when.Add(time.Duration(i) * time.Hour)
				r, err := s.StartRun(ctx, c.source, []*config.Context{testContext("prod")}, at)
				if err != nil {
					t.Fatal(err)
				}
				payload, err := json.Marshal(c.host)
				if err != nil {
					t.Fatal(err)
				}
				hosts := []ResourceObservation{{VCenterID: "vc-1", Context: "prod", Kind: "host", ID: c.host.ID, Name: c.host.Name, Payload: payload}}
				result := ContextResult{Name: "prod", VCenterID: "vc-1", Status: "success",
					VMs:         []Observation{{VCenterID: "vc-1", Context: "prod", VM: c.vm}},
					Collections: []CollectionResult{{Kind: "host", Status: "success", ItemCount: 1, Resources: hosts}}}
				if err := s.SaveContext(ctx, r.ID, result, at.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				if _, err := s.FinishRun(ctx, r.ID, at.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				ids[i] = r.ID
			}
			d, err := s.Diff(ctx, ids[0], ids[1], false)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, c := range d.VMs {
				for _, f := range c.Fields {
					got = append(got, f.Field)
				}
			}
			for _, c := range d.Resources {
				for _, f := range c.Fields {
					got = append(got, c.Kind+"."+f.Field)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("changed fields = %v, want %v", got, tc.want)
			}
			leftOut := slices.ContainsFunc(d.Warnings, func(w string) bool {
				return strings.Contains(w, "not compared") && strings.Contains(w, "migration_configuration.firmware") && strings.Contains(w, "path")
			})
			if leftOut != tc.wantLeftOut {
				t.Fatalf("warning naming the fields left out = %v, want %v; warnings %q", leftOut, tc.wantLeftOut, d.Warnings)
			}
		})
	}
}
