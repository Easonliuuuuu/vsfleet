package demo

import (
	"context"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/health"
)

func TestNewBackendIncludesOrphanAndZombieFixtures(t *testing.T) {
	backend := NewBackend()
	inv := backend.inventories["prod-vc"]
	if inv == nil {
		t.Fatal("prod-vc demo inventory is missing")
	}
	foundOrphan := false
	for _, vm := range inv.VMs {
		foundOrphan = foundOrphan || vm.ConnectionState == "orphaned"
	}
	if !foundOrphan {
		t.Fatal("demo inventory has no orphaned VM")
	}
	if len(inv.Datastores) == 0 || inv.Datastores[0].BrowseStatus != "success" {
		t.Fatalf("demo datastore browse provenance=%+v", inv.Datastores)
	}
	foundZombie := false
	for _, file := range inv.Datastores[0].Files {
		if file.Path == "[nvme-01] lost+found/orphan.vmdk" {
			foundZombie = true
		}
	}
	if !foundZombie {
		t.Fatal("demo inventory has no zombie VMDK fixture")
	}
}

func TestAssessmentServiceSeedsNewHealthFindingsInMemory(t *testing.T) {
	service, closeStore, err := NewBackend().AssessmentService()
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore()
	runID, err := service.Store.ResolveRun(context.Background(), "latest")
	if err != nil {
		t.Fatal(err)
	}
	data, err := service.Store.LoadExportData(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	report := health.Evaluate(data, health.Options{Thresholds: health.DefaultThresholds()})
	found := map[string]bool{}
	for _, finding := range report.Findings {
		found[finding.Rule] = true
	}
	if !found["vm-orphaned"] || !found["datastore-zombie-vmdk"] {
		t.Fatalf("demo health findings=%v", found)
	}
	orphans := health.Orphans(data)
	confidence := map[health.Confidence]bool{}
	for _, entry := range orphans.Entries {
		confidence[entry.Confidence] = true
	}
	if !confidence[health.ConfidenceVerified] || !confidence[health.ConfidenceOtherContext] {
		t.Fatalf("demo orphan confidence=%v entries=%+v", confidence, orphans.Entries)
	}
}
