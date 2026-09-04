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
	run, err := s.StartRun(context.Background(), "test", []*config.Context{{Name: "prod", Endpoint: "https://vc.example", Datacenter: "dc-a"}}, when)
	if err != nil {
		t.Fatal(err)
	}
	vm := vsphere.VM{ID: "vm-1", Name: "app", Snapshots: []vsphere.VMSnapshot{{ID: "snap-1", Name: "base", CreateTime: when}}}
	if err := s.SaveContext(context.Background(), run.ID, ContextResult{Name: "prod", VCenterID: "vc-uuid", Status: "success", VMs: []Observation{{Context: "prod", VCenterID: "vc-uuid", VM: vm}}, Collections: []CollectionResult{{Kind: "vm", Status: "success", ItemCount: 1}, {Kind: "host", Status: "empty"}, {Kind: "cluster", Status: "empty"}, {Kind: "datastore", Status: "empty"}}}, when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishRun(context.Background(), run.ID, when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	data, err := s.LoadExportData(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Contexts) != 1 || len(data.VMs) != 1 || len(data.VMs[0].Snapshots) != 1 {
		t.Fatalf("export data=%+v", data)
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
