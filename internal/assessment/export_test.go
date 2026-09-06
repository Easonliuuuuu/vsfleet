package assessment

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestLoadExportDataUsesPersistedEvidence(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	run, err := s.StartRunWithMetadata(context.Background(), "test", []*config.Context{{Name: "prod", Endpoint: "https://vc.example", Datacenter: "dc-a"}}, when, RunMetadata{InventorySchemaVersion: CurrentInventorySchemaVersion})
	if err != nil {
		t.Fatal(err)
	}
	connected := true
	vm := vsphere.VM{ID: "vm-1", Name: "app", Disks: []vsphere.VMDisk{{Key: 101, Label: "Hard disk 1", CapacityBytes: 8 << 30}}, NICs: []vsphere.VMNIC{{Key: 201, Label: "Network adapter 1", Network: "VM Network", Connected: &connected, IPv4: []string{"192.0.2.20"}}}, CDROMs: []vsphere.VMCDROM{{Key: 301, Label: "CD/DVD drive 1", Connected: &connected, BackingType: "iso", BackingPath: "[ds] app/install.iso"}}, USBs: []vsphere.VMUSB{{Key: 401, Label: "USB device 1", Connected: &connected, BackingType: "remoteHost", BackingHost: "esx-1"}}, Snapshots: []vsphere.VMSnapshot{{ID: "snap-1", Name: "base", CreateTime: when}}}
	template := vsphere.VM{ID: "tpl-1", Name: "golden", IsTemplate: true}
	if err := s.SaveContext(context.Background(), run.ID, ContextResult{Name: "prod", VCenterID: "vc-uuid", Status: "success", VMs: []Observation{{Context: "prod", VCenterID: "vc-uuid", VM: vm}, {Context: "prod", VCenterID: "vc-uuid", VM: template}}, Collections: []CollectionResult{{Kind: "vm", Status: "success", ItemCount: 2}, {Kind: "host", Status: "empty"}, {Kind: "cluster", Status: "empty"}, {Kind: "datastore", Status: "empty"}}}, when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishRun(context.Background(), run.ID, when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	data, err := s.LoadExportData(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Contexts) != 1 || len(data.VMs) != 2 || len(data.VMs[0].Snapshots) != 1 {
		t.Fatalf("export data=%+v", data)
	}
	if len(data.VMs[0].Observation.VM.Disks) != 1 || len(data.VMs[0].Observation.VM.NICs) != 1 || data.VMs[0].Observation.VM.NICs[0].Network != "VM Network" {
		t.Fatalf("device evidence was not persisted: %+v", data.VMs[0].Observation.VM)
	}
	if len(data.VMs[0].Observation.VM.CDROMs) != 1 || len(data.VMs[0].Observation.VM.USBs) != 1 || data.VMs[0].Observation.VM.CDROMs[0].BackingPath == "" || data.VMs[0].Observation.VM.USBs[0].BackingHost != "esx-1" {
		t.Fatalf("connected-device evidence was not persisted: %+v", data.VMs[0].Observation.VM)
	}
	if data.Contexts[0].Endpoint != "https://vc.example" || data.VMs[0].Observation.VCenterID != "vc-uuid" {
		t.Fatalf("provenance=%+v", data)
	}
}

func TestLoadExportDataRejectsRunningAndMalformed(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	run, err := s.StartRun(context.Background(), "test", []*config.Context{{Name: "prod", Endpoint: "https://vc.example"}}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadExportData(context.Background(), run.ID); err == nil {
		t.Fatal("running export unexpectedly succeeded")
	}
	if err := s.SaveContext(context.Background(), run.ID, ContextResult{Name: "prod", VCenterID: "vc-uuid", Status: "success", VMs: []Observation{{Context: "prod", VCenterID: "vc-uuid", VM: vsphere.VM{ID: "vm-1", Name: "app"}}}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishRun(context.Background(), run.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	// A malformed persisted payload must remain an explicit export error.
	if _, err := s.db.Exec(`UPDATE vm_observations SET payload=?`, json.RawMessage("{")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadExportData(context.Background(), run.ID); err == nil {
		t.Fatal("malformed export unexpectedly succeeded")
	}
}
