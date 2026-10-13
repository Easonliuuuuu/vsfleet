package assessment

import (
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func stored(name, vc, moref, instance string) storedVM {
	return storedVM{observation: Observation{VCenterID: vc, Context: vc, VM: vsphere.VM{ID: moref, Name: name, InstanceUUID: instance}}}
}

func TestCompareVMsDetectsIdentityAwareMove(t *testing.T) {
	base := []storedVM{stored("billing", "vc-a", "vm-1", "instance-1")}
	target := []storedVM{stored("billing", "vc-b", "vm-9", "instance-1")}
	changes, _ := compareVMs(base, target, false)
	if len(changes) != 1 || changes[0].Changes[0] != "moved" || changes[0].MatchBasis != "instance_uuid" {
		t.Fatalf("changes=%+v", changes)
	}
}

func TestCompareVMsRejectsAmbiguousIdentity(t *testing.T) {
	base := []storedVM{
		stored("one", "vc-a", "vm-1", "duplicate"),
		stored("two", "vc-a", "vm-2", "duplicate"),
	}
	target := []storedVM{stored("replacement", "vc-a", "vm-9", "duplicate")}
	changes, _ := compareVMs(base, target, false)
	if len(changes) != 3 {
		t.Fatalf("changes=%+v", changes)
	}
	if changes[0].Changes[0] != "appeared" {
		t.Fatalf("ambiguous target was paired: %+v", changes)
	}
}

func TestCompareVMsDetectsMigrationConfigurationChange(t *testing.T) {
	base := stored("app", "vc-a", "vm-1", "instance-1")
	base.observation.VM.ConfigurationAvailable = true
	base.observation.VM.Firmware = "efi"
	target := base
	target.observation.VM.Firmware = "bios"
	changes, _ := compareVMs([]storedVM{base}, []storedVM{target}, false)
	if len(changes) != 1 || len(changes[0].Fields) != 1 || changes[0].Fields[0].Field != "migration_configuration.firmware" {
		t.Fatalf("migration configuration change=%+v", changes)
	}
}

func TestChangedFieldsReportsMetadataByStableIdentity(t *testing.T) {
	base := vsphere.VM{Metadata: vsphere.Metadata{
		TagsStatus: "available", CustomAttributesStatus: "available",
		Tags:             []vsphere.Tag{{ID: "tag-a", CategoryID: "cat-a", Category: "Environment", Name: "Production"}},
		CustomAttributes: []vsphere.CustomAttribute{{Key: 7, Name: "owner", Value: "platform"}},
	}}
	target := base
	target.Metadata.Tags = []vsphere.Tag{{ID: "tag-a", CategoryID: "cat-a", Category: "Environment", Name: "Production"}, {ID: "tag-b", CategoryID: "cat-b", Category: "Environment", Name: "Production"}}
	target.Metadata.CustomAttributes = []vsphere.CustomAttribute{{Key: 7, Name: "owner", Value: "compute"}}
	fields := changedFields(base, target, false)
	if len(fields) != 2 {
		t.Fatalf("metadata fields=%+v", fields)
	}
	if fields[0].Field != "metadata.custom_attributes[7]" || fields[0].Before != "owner=platform" || fields[0].After != "owner=compute" {
		t.Fatalf("custom attribute change=%+v", fields[0])
	}
	if fields[1].Field != "metadata.tags[cat-b/tag-b]" || fields[1].After != "Environment/Production" {
		t.Fatalf("tag change=%+v", fields[1])
	}
}

func TestChangedFieldsDoesNotInferUnavailableMetadataChanges(t *testing.T) {
	base := vsphere.VM{Metadata: vsphere.Metadata{TagsStatus: "unavailable"}}
	target := vsphere.VM{Metadata: vsphere.Metadata{TagsStatus: "available", Tags: []vsphere.Tag{{ID: "tag", Name: "Production"}}}}
	if fields := changedFields(base, target, false); len(fields) != 0 {
		t.Fatalf("unavailable metadata should remain unknown, got %+v", fields)
	}
}

func TestMigrationFieldChangesNameTheDiffSubField(t *testing.T) {
	old := vsphere.VM{Disks: []vsphere.VMDisk{{Key: 2000, CapacityBytes: 10, SharedBus: "noSharing"}}}
	now := vsphere.VM{Disks: []vsphere.VMDisk{{Key: 2000, CapacityBytes: 10, SharedBus: "physicalSharing"}}}
	got := migrationFieldChanges(old, now)
	if len(got) != 1 || got[0].Field != "migration_configuration.disks[2000].shared_bus" || got[0].Before != `"noSharing"` || got[0].After != `"physicalSharing"` {
		t.Fatalf("changes=%+v", got)
	}
	capacity := old
	capacity.Disks = []vsphere.VMDisk{{Key: 2000, CapacityBytes: 20, SharedBus: "noSharing"}}
	if got := migrationFieldChanges(old, capacity); len(got) != 1 || got[0].Field != "migration_configuration.disks[2000].capacity_bytes" || got[0].Before != "10" || got[0].After != "20" {
		t.Fatalf("capacity changes=%+v", got)
	}
	cosmetic := old
	cosmetic.Disks = []vsphere.VMDisk{{Key: 2000, CapacityBytes: 10, SharedBus: "noSharing", Label: "renamed", BackingPath: "[ds] moved/disk.vmdk"}}
	if got := migrationFieldChanges(old, cosmetic); len(got) != 0 {
		t.Fatalf("cosmetic fields read as migration changes: %+v", got)
	}
	if got := migrationFieldChanges(vsphere.VM{Disks: []vsphere.VMDisk{{Key: 2000}}}, vsphere.VM{Disks: []vsphere.VMDisk{{Key: 2000, SharedBus: "noSharing"}}}); len(got) != 0 {
		t.Fatalf("late field read as change: %+v", got)
	}
}
