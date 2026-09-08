package vsphere

import (
	"testing"

	"github.com/vmware/govmomi/vim25/types"
)

func TestMapHostMultipathsPreservesStorageLocality(t *testing.T) {
	local, shared := true, false
	storage := &types.HostStorageDeviceInfo{ScsiLun: []types.BaseScsiLun{
		&types.HostScsiDisk{ScsiLun: types.ScsiLun{Key: "local-key", Uuid: "local-uuid", CanonicalName: "naa.local"}, LocalDisk: &local},
		&types.HostScsiDisk{ScsiLun: types.ScsiLun{Key: "shared-key", Uuid: "shared-uuid", CanonicalName: "naa.shared"}, LocalDisk: &shared},
		&types.ScsiLun{Key: "unknown-key", Uuid: "unknown-uuid", CanonicalName: "naa.unknown"},
	}, MultipathInfo: &types.HostMultipathInfo{Lun: []types.HostMultipathInfoLogicalUnit{
		{Key: "local-mp", Lun: "local-key"},
		{Key: "shared-mp", Lun: "shared-uuid"},
		{Key: "unknown-mp", Lun: "naa.unknown"},
	}}}

	got := mapHostMultipaths(storage, storage.MultipathInfo)
	if len(got) != 3 {
		t.Fatalf("mapped %d multipaths, want 3", len(got))
	}
	for _, path := range got {
		switch path.Key {
		case "local-mp":
			if path.LocalDisk == nil || !*path.LocalDisk {
				t.Fatalf("local path locality=%v", path.LocalDisk)
			}
		case "shared-mp":
			if path.LocalDisk == nil || *path.LocalDisk {
				t.Fatalf("shared path locality=%v", path.LocalDisk)
			}
		case "unknown-mp":
			if path.LocalDisk != nil {
				t.Fatalf("unknown path locality=%v", path.LocalDisk)
			}
		default:
			t.Fatalf("unexpected path %q", path.Key)
		}
	}
}
