package vsphere

import (
	"reflect"
	"testing"

	"github.com/vmware/govmomi/vim25/types"
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
	for _, property := range []string{"config.hardware.device", "guest.net"} {
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
