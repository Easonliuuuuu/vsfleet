package vsphere

import (
	"reflect"
	"testing"

	"github.com/vmware/govmomi/vim25/types"
)

// The LUN and path are esxi01's local-esxi01 disk on the nested lab's ESXi
// 8.0.3, whose padded vendor, model and serial RVTools writes unchanged.
// A second path through an adapter the host does not list is named by its
// runtime name's adapter part.
func TestMapHostMultipathsReadsTheLUNAndItsPaths(t *testing.T) {
	local := true
	working := true
	lun := types.ScsiLun{Key: "key-vim.host.ScsiDisk-0100", Uuid: "01000000006573786930316c6f63616c20202020202020202051454d552048",
		CanonicalName: "t10.ATA_____QEMU_HARDDISK___________________________esxi01local_________", DisplayName: "Local ATA Disk (t10.ATA_____QEMU_HARDDISK___________________________esxi01local_________)",
		OperationalState: []string{"ok"}, VStorageSupport: "vStorageUnsupported", QueueDepth: 31, ScsiLevel: 5,
		Vendor: "ATA     ", Model: "QEMU HARDDISK   ", Revision: "2.5+", SerialNumber: "esxi01local         "}
	storage := &types.HostStorageDeviceInfo{
		HostBusAdapter: []types.BaseHostHostBusAdapter{&types.HostBlockHba{HostHostBusAdapter: types.HostHostBusAdapter{Key: "key-vim.host.BlockHba-vmhba0", Device: "vmhba0"}}},
		ScsiLun:        []types.BaseScsiLun{&types.HostScsiDisk{ScsiLun: lun, LocalDisk: &local}},
		MultipathInfo: &types.HostMultipathInfo{Lun: []types.HostMultipathInfoLogicalUnit{{Key: "mp-1", Lun: lun.Uuid,
			Policy: &types.HostMultipathInfoFixedLogicalUnitPolicy{HostMultipathInfoLogicalUnitPolicy: types.HostMultipathInfoLogicalUnitPolicy{Policy: "FIXED"}},
			Path: []types.HostMultipathInfoPath{
				{Name: "vmhba0:C0:T2:L0", PathState: "active", State: "active", Adapter: "key-vim.host.BlockHba-vmhba0", IsWorkingPath: &working},
				{Name: "vmhba64:C0:T2:L0", PathState: "dead", Adapter: "key-vim.host.FibreChannelHba-vmhba64"},
			}}}},
	}
	got := mapHostMultipaths(storage, storage.MultipathInfo)
	if len(got) != 1 {
		t.Fatalf("mapped %d multipaths, want 1", len(got))
	}
	depth, level := int32(31), int32(5)
	want := &HostMultipathLUN{CanonicalName: lun.CanonicalName, OperationalState: "ok", VStorageSupport: "vStorageUnsupported", QueueDepth: &depth,
		Vendor: "ATA     ", Model: "QEMU HARDDISK   ", Revision: "2.5+", SCSILevel: &level, Serial: "esxi01local         ", UUID: lun.Uuid,
		Paths: []HostMultipathPath{{Name: "vmhba0:C0:T2:L0", Adapter: "vmhba0", State: "active"}, {Name: "vmhba64:C0:T2:L0", Adapter: "vmhba64", State: "dead"}}}
	if !reflect.DeepEqual(got[0].Device, want) {
		t.Errorf("device = %+v, want %+v", got[0].Device, want)
	}
	if got[0].Active != 1 || got[0].Dead != 1 || got[0].WorkingPaths != 1 {
		t.Errorf("counts = %+v, want one active, one dead, one working", got[0])
	}
}
