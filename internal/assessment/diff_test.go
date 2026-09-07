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
	if len(changes) != 1 || len(changes[0].Fields) != 1 || changes[0].Fields[0].Field != "migration_configuration" {
		t.Fatalf("migration configuration change=%+v", changes)
	}
}
