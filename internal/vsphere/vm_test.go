package vsphere

import (
	"reflect"
	"testing"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/easonliuuuuu/vsfleet/internal/config"
)

func TestWalkDevicesExtractsDisksAndGuestNetworks(t *testing.T) {
	thin, eager, split, writeThrough := true, false, false, true
	connected, starts := true, true
	controllerDesc := &types.Description{Label: "SCSI controller 0"}
	controller := &types.VirtualLsiLogicController{VirtualSCSIController: types.VirtualSCSIController{
		VirtualController:  types.VirtualController{VirtualDevice: types.VirtualDevice{Key: 100, DeviceInfo: controllerDesc}},
		SharedBus:          types.VirtualSCSISharing("noSharing"),
		ScsiCtlrUnitNumber: 7,
	}}
	disk := &types.VirtualDisk{VirtualDevice: types.VirtualDevice{
		Key: 101, DeviceInfo: &types.Description{Label: "Hard disk 1"}, ControllerKey: 100,
		UnitNumber: &[]int32{0}[0],
		Backing: &types.VirtualDiskFlatVer2BackingInfo{
			VirtualDeviceFileBackingInfo: types.VirtualDeviceFileBackingInfo{FileName: "[datastore-1] app/app.vmdk"},
			DiskMode:                     "persistent", ThinProvisioned: &thin, EagerlyScrub: &eager, Split: &split, WriteThrough: &writeThrough,
			Uuid: "disk-uuid", Sharing: "noSharing",
		},
	}, CapacityInBytes: 8 << 30}
	card := types.VirtualEthernetCard{
		VirtualDevice: types.VirtualDevice{Key: 200, DeviceInfo: &types.Description{Label: "Network adapter 1"}, Connectable: &types.VirtualDeviceConnectInfo{Connected: connected, StartConnected: starts}},
		AddressType:   "assigned", MacAddress: "00:50:56:AA:BB:CC",
	}
	card.Backing = &types.VirtualEthernetCardNetworkBackingInfo{VirtualDeviceDeviceBackingInfo: types.VirtualDeviceDeviceBackingInfo{DeviceName: "VM Network"}, Network: &types.ManagedObjectReference{Type: "Network", Value: "network-1"}}
	nic := &types.VirtualVmxnet3{VirtualVmxnet: types.VirtualVmxnet{VirtualEthernetCard: card}}
	guest := &types.GuestInfo{Net: []types.GuestNicInfo{{DeviceConfigId: 200, Network: "guest-portgroup", MacAddress: "00:50:56:aa:bb:cc", IpAddress: []string{"192.0.2.20", "2001:db8::20"}}}}
	idx := &index{byRef: map[types.ManagedObjectReference]entity{{Type: "Network", Value: "network-1"}: {name: "VM Network"}}}

	disks, nics := walkDevices([]types.BaseVirtualDevice{nic, disk, controller}, guest, idx)
	if len(disks) != 1 || len(nics) != 1 {
		t.Fatalf("devices = %d disks, %d nics; want one each", len(disks), len(nics))
	}
	gotDisk := disks[0]
	if gotDisk.Label != "Hard disk 1" || gotDisk.Controller != "LSI Logic" || gotDisk.ControllerLabel != "SCSI controller 0" || gotDisk.BackingPath == "" || gotDisk.UUID != "disk-uuid" {
		t.Errorf("disk normalization = %+v", gotDisk)
	}
	if gotDisk.ThinProvisioned == nil || !*gotDisk.ThinProvisioned || gotDisk.EagerlyScrub == nil || *gotDisk.EagerlyScrub {
		t.Errorf("disk optional flags = %+v", gotDisk)
	}
	gotNIC := nics[0]
	if gotNIC.Adapter != "Vmxnet3" || gotNIC.Network != "guest-portgroup" || gotNIC.NetworkID != "network-1" || gotNIC.MACAddress != "00:50:56:AA:BB:CC" {
		t.Errorf("nic normalization = %+v", gotNIC)
	}
	if !reflect.DeepEqual(gotNIC.IPv4, []string{"192.0.2.20"}) || !reflect.DeepEqual(gotNIC.IPv6, []string{"2001:db8::20"}) {
		t.Errorf("guest addresses = v4 %v, v6 %v", gotNIC.IPv4, gotNIC.IPv6)
	}
	if gotNIC.Connected == nil || !*gotNIC.Connected || gotNIC.StartsConnected == nil || !*gotNIC.StartsConnected {
		t.Errorf("nic connection flags = %+v", gotNIC)
	}
}

func TestVMPropertiesIncludeDeviceWalk(t *testing.T) {
	for _, property := range []string{"config.hardware.device", "guest.net", "guest.disk", "guest.toolsVersion", "guest.toolsVersionStatus2"} {
		found := false
		for _, candidate := range vmProps {
			if candidate == property {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("vmProps does not include %q: %v", property, vmProps)
		}
	}
}

func TestNewVMCopiesToolsVersion(t *testing.T) {
	m := &mo.VirtualMachine{
		Guest: &types.GuestInfo{
			ToolsRunningStatus:  "guestToolsRunning",
			ToolsVersion:        "12352",
			ToolsVersionStatus2: "guestToolsCurrent",
		},
	}
	vm := newVM(&Client{Context: &config.Context{Name: "prod"}}, &index{}, m)
	if vm.ToolsState != "guestToolsRunning" {
		t.Errorf("tools state = %q", vm.ToolsState)
	}
	if vm.ToolsVersion != "12352" {
		t.Errorf("tools version = %q", vm.ToolsVersion)
	}
	if vm.ToolsVersionStatus != "guestToolsCurrent" {
		t.Errorf("tools version status = %q", vm.ToolsVersionStatus)
	}
}

func TestNewVMCopiesGuestPartitions(t *testing.T) {
	m := &mo.VirtualMachine{
		Guest: &types.GuestInfo{
			ToolsRunningStatus: "guestToolsRunning",
			Disk: []types.GuestDiskInfo{
				// Out of order, and one with no path: vPartition rows are
				// keyed on the path downstream, so a blank one is dropped and
				// the rest sort so repeated exports stay byte-identical.
				{DiskPath: "/var", Capacity: 4 << 30, FreeSpace: 1 << 30, FilesystemType: "xfs"},
				{Capacity: 1 << 30},
				{DiskPath: "/", Capacity: 8 << 30, FreeSpace: 2 << 30, FilesystemType: "ext4"},
			},
		},
	}
	vm := newVM(&Client{Context: &config.Context{Name: "prod"}}, &index{}, m)
	want := []VMPartition{
		{Path: "/", CapacityBytes: 8 << 30, FreeBytes: 2 << 30, FilesystemType: "ext4"},
		{Path: "/var", CapacityBytes: 4 << 30, FreeBytes: 1 << 30, FilesystemType: "xfs"},
	}
	if !reflect.DeepEqual(vm.Partitions, want) {
		t.Errorf("partitions = %+v, want %+v", vm.Partitions, want)
	}
}

// The mapping back to virtual disks is what lets a sizing tool tie consumed
// space to the disk it has to provision. Tools reports it only on vSphere 7.0
// and later, so a partition without one is an older estate, not a filesystem
// with no disk behind it — and a spanned volume names every disk it crosses.
func TestNewVMCopiesGuestPartitionDiskKeys(t *testing.T) {
	m := &mo.VirtualMachine{
		Guest: &types.GuestInfo{
			ToolsRunningStatus: "guestToolsRunning",
			Disk: []types.GuestDiskInfo{
				{DiskPath: "/", Capacity: 8 << 30, Mappings: []types.GuestInfoVirtualDiskMapping{{Key: 2000}}},
				// Out of order, so repeated exports stay byte-identical.
				{DiskPath: "/data", Capacity: 4 << 30, Mappings: []types.GuestInfoVirtualDiskMapping{{Key: 2003}, {Key: 2001}}},
				{DiskPath: "/legacy", Capacity: 1 << 30},
			},
		},
	}
	vm := newVM(&Client{Context: &config.Context{Name: "prod"}}, &index{}, m)
	want := [][]int32{{2000}, {2001, 2003}, nil}
	for i, part := range vm.Partitions {
		if !reflect.DeepEqual(part.DiskKeys, want[i]) {
			t.Errorf("%s disk keys = %v, want %v", part.Path, part.DiskKeys, want[i])
		}
	}
}

// A guest without running Tools reports nothing. That must stay nil rather
// than becoming a zero-capacity partition, which would read downstream as a
// full disk rather than as an unanswered one.
func TestNewVMLeavesPartitionsNilWithoutGuestData(t *testing.T) {
	for _, tc := range []struct {
		name  string
		guest *types.GuestInfo
	}{
		{name: "no guest at all", guest: nil},
		{name: "tools not running", guest: &types.GuestInfo{ToolsRunningStatus: "guestToolsNotRunning"}},
		{name: "only unnamed disks", guest: &types.GuestInfo{Disk: []types.GuestDiskInfo{{Capacity: 1 << 30}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := newVM(&Client{Context: &config.Context{Name: "prod"}}, &index{}, &mo.VirtualMachine{Guest: tc.guest})
			if vm.Partitions != nil {
				t.Errorf("partitions = %+v, want nil", vm.Partitions)
			}
		})
	}
}

func TestVMPartitionUsedBytes(t *testing.T) {
	for _, tc := range []struct {
		name string
		part VMPartition
		want int64
	}{
		{name: "capacity minus free", part: VMPartition{CapacityBytes: 8 << 30, FreeBytes: 2 << 30}, want: 6 << 30},
		{name: "unsized partition", part: VMPartition{}, want: 0},
		// Tools has been seen reporting free space above capacity; consumed
		// must not go negative and skew a sizing total.
		{name: "free exceeds capacity", part: VMPartition{CapacityBytes: 1 << 30, FreeBytes: 2 << 30}, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.part.UsedBytes(); got != tc.want {
				t.Errorf("UsedBytes() = %d, want %d", got, tc.want)
			}
		})
	}
}
