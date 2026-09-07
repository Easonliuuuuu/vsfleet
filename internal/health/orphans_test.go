package health

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func orphanResource(t *testing.T, context, vc string, datastore vsphere.Datastore) assessment.ResourceObservation {
	t.Helper()
	payload, err := json.Marshal(datastore)
	if err != nil {
		t.Fatal(err)
	}
	return assessment.ResourceObservation{Context: context, VCenterID: vc, Kind: "datastore", ID: datastore.ID, Name: datastore.Name, Payload: payload}
}

func completeOrphanContext(name string) assessment.ContextRun {
	return assessment.ContextRun{Name: name, VMStatus: "success", Collections: []assessment.CollectionRun{{Kind: "vm", Status: "success"}, {Kind: "datastore", Status: "success"}}}
}

func TestOrphansClassifiesSharedStorageAndBothSnapshotDirections(t *testing.T) {
	finish := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	prod := vsphere.Datastore{Location: vsphere.Location{Context: "prod", Datacenter: "dc-a"}, ID: "ds-prod", Name: "datastore1", Backing: vsphere.DatastoreBacking{VMFSUUID: "uuid-1", Extents: []string{"naa.6000"}}, BrowseStatus: "success", Files: []vsphere.DatastoreFile{
		{Path: "[datastore1] app/disk.vmdk"},
		{Path: "[datastore1] app/disk-000002.vmdk"},
		{Path: "[datastore1] lost/orphan.vmdk"},
		{Path: "[datastore1] lost/orphan-flat.vmdk"},
	}}
	edge := vsphere.Datastore{Location: vsphere.Location{Context: "edge", Datacenter: "dc-b"}, ID: "ds-edge", Name: "other-name", Backing: vsphere.DatastoreBacking{Extents: []string{"naa.6000"}}, BrowseStatus: "success"}
	data := assessment.ExportData{
		Run:      assessment.Run{ID: 1, InventorySchemaVersion: "11", FinishedAt: finish},
		Contexts: []assessment.ContextRun{completeOrphanContext("prod"), completeOrphanContext("edge")},
		VMs: []assessment.ExportVM{
			{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-prod", VM: vsphere.VM{Name: "snapshot-vm", Disks: []vsphere.VMDisk{{BackingPath: "[datastore1] app/disk-000002.vmdk"}}}}},
			{Observation: assessment.Observation{Context: "edge", VCenterID: "vc-edge", VM: vsphere.VM{Name: "finance", Disks: []vsphere.VMDisk{{BackingPath: "[other-name] lost/orphan.vmdk"}}}}},
		},
		Resources: []assessment.ResourceObservation{orphanResource(t, "prod", "vc-prod", prod), orphanResource(t, "edge", "vc-edge", edge)},
	}
	report := Orphans(data)
	byPath := make(map[string]OrphanEvidence, len(report.Entries))
	for _, entry := range report.Entries {
		byPath[entry.Path] = entry
	}
	if _, ok := byPath["[datastore1] app/disk.vmdk"]; ok {
		t.Fatal("snapshot base was incorrectly classified as an orphan")
	}
	if _, ok := byPath["[datastore1] app/disk-000002.vmdk"]; ok {
		t.Fatal("snapshot delta was incorrectly classified as an orphan")
	}
	orphan, ok := byPath["[datastore1] lost/orphan.vmdk"]
	if !ok || orphan.Confidence != ConfidenceOtherContext {
		t.Fatalf("shared orphan=%+v, want referenced-other-context", orphan)
	}
}

func TestOrphansDowngradesTruncatedAndBlindCoverage(t *testing.T) {
	finish := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	truncated := vsphere.Datastore{Location: vsphere.Location{Context: "prod"}, ID: "ds-1", Name: "prod", Backing: vsphere.DatastoreBacking{VMFSUUID: "uuid-1"}, BrowseStatus: "success", BrowseTruncated: true, Files: []vsphere.DatastoreFile{{Path: "[prod] orphan.vmdk"}}}
	blind := vsphere.Datastore{Location: vsphere.Location{Context: "blind"}, ID: "ds-2", Name: "blind", Backing: vsphere.DatastoreBacking{VMFSUUID: "uuid-2"}, BrowseStatus: "success", Files: []vsphere.DatastoreFile{{Path: "[blind] orphan.vmdk"}}}
	data := assessment.ExportData{Run: assessment.Run{ID: 2, InventorySchemaVersion: "11", FinishedAt: finish}, Contexts: []assessment.ContextRun{completeOrphanContext("prod"), {Name: "blind", VMStatus: "failed", Collections: []assessment.CollectionRun{{Kind: "vm", Status: "failed"}, {Kind: "datastore", Status: "success"}}}}, Resources: []assessment.ResourceObservation{orphanResource(t, "prod", "vc-prod", truncated), orphanResource(t, "blind", "vc-blind", blind)}}
	report := Orphans(data)
	for _, entry := range report.Entries {
		if entry.Confidence != ConfidenceUnknown {
			t.Errorf("%s confidence=%s, want unknown", entry.Path, entry.Confidence)
		}
	}
	if len(report.Entries) != 2 {
		t.Fatalf("entries=%d, want 2", len(report.Entries))
	}
}

func TestOrphansDoesNotCrossSuppressSameNameDatastores(t *testing.T) {
	left := vsphere.Datastore{Location: vsphere.Location{Context: "left"}, ID: "ds-left", Name: "datastore1", Backing: vsphere.DatastoreBacking{Extents: []string{"naa.left"}}, BrowseStatus: "success", Files: []vsphere.DatastoreFile{{Path: "[datastore1] orphan.vmdk"}}}
	right := vsphere.Datastore{Location: vsphere.Location{Context: "right"}, ID: "ds-right", Name: "datastore1", Backing: vsphere.DatastoreBacking{Extents: []string{"naa.right"}}, BrowseStatus: "success", Files: []vsphere.DatastoreFile{{Path: "[datastore1] orphan.vmdk"}}}
	data := assessment.ExportData{Run: assessment.Run{InventorySchemaVersion: "11"}, Contexts: []assessment.ContextRun{completeOrphanContext("left"), completeOrphanContext("right")}, Resources: []assessment.ResourceObservation{orphanResource(t, "left", "vc-left", left), orphanResource(t, "right", "vc-right", right)}}
	if got := len(Orphans(data).Entries); got != 2 {
		t.Fatalf("same-name independent datastores produced %d entries, want 2", got)
	}
}
