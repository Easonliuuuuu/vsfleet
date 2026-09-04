package vsphere

import (
	"context"
	"net"
	"sort"
	"strings"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

var vmProps = []string{
	"name",
	"parent",
	"config.template",
	"config.uuid",
	"config.instanceUuid",
	"config.guestFullName",
	"config.annotation",
	"config.hardware.numCPU",
	"config.hardware.memoryMB",
	"config.hardware.device",
	"runtime.powerState",
	"runtime.host",
	"guest.ipAddress",
	"guest.net",
	"guest.guestState",
	"guest.toolsRunningStatus",
	"guest.toolsVersion",
	"guest.toolsVersionStatus2",
	"summary.storage.committed",
	"datastore",
	"snapshot",
}

// ListVMs returns the virtual machines in a vCenter, excluding templates.
func (c *Client) ListVMs(ctx context.Context) ([]VM, error) {
	all, err := c.listAllVMs(ctx)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, vm := range all {
		if !vm.IsTemplate {
			out = append(out, vm)
		}
	}
	return out, nil
}

func (c *Client) listAllVMs(ctx context.Context) ([]VM, error) {
	idx, err := newIndex(ctx, c)
	if err != nil {
		return nil, err
	}
	return c.listVMs(ctx, idx)
}

func (c *Client) listVMs(ctx context.Context, idx *index) ([]VM, error) {
	var raw []mo.VirtualMachine
	if err := retrieve(ctx, c, idx.root, []string{"VirtualMachine"}, []string{"VirtualMachine"}, vmProps, &raw); err != nil {
		return nil, err
	}
	out := make([]VM, 0, len(raw))
	for i := range raw {
		out = append(out, newVM(c, idx, &raw[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func newVM(c *Client, idx *index, m *mo.VirtualMachine) VM {
	loc := idx.locate(c, m.Self, m.Name)
	vm := VM{
		Location:   loc,
		ID:         m.Self.Value,
		Name:       m.Name,
		PowerState: string(m.Runtime.PowerState),
		Host:       idx.name(m.Runtime.Host),
		Cluster:    idx.clusterOf(m.Runtime.Host),
		Folder:     idx.folderPath(m.Parent, loc.Datacenter),
		Datastores: idx.names(m.Datastore),
	}
	if cfg := m.Config; cfg != nil {
		vm.IsTemplate = cfg.Template
		vm.BIOSUUID = cfg.Uuid
		vm.InstanceUUID = cfg.InstanceUuid
		vm.GuestOS = cfg.GuestFullName
		vm.Annotation = strings.TrimSpace(cfg.Annotation)
		vm.CPU = cfg.Hardware.NumCPU
		vm.MemoryMB = int64(cfg.Hardware.MemoryMB)
		vm.Disks, vm.NICs = walkDevices(cfg.Hardware.Device, m.Guest, idx)
	}
	if snap := m.Snapshot; snap != nil {
		vm.Snapshots = flattenSnapshots(snap.RootSnapshotList, "", snap.CurrentSnapshot)
	}
	if g := m.Guest; g != nil {
		vm.IPAddress = g.IpAddress
		vm.GuestState = g.GuestState
		vm.ToolsState = g.ToolsRunningStatus
		vm.ToolsVersion = g.ToolsVersion
		vm.ToolsVersionStatus = g.ToolsVersionStatus2
		if vm.GuestOS == "" {
			vm.GuestOS = g.GuestFullName
		}
	}
	if s := m.Summary.Storage; s != nil {
		vm.StorageGB = float64(s.Committed) / (1 << 30)
	}
	return vm
}

func walkDevices(devices []types.BaseVirtualDevice, guest *types.GuestInfo, idx *index) ([]VMDisk, []VMNIC) {
	controllers := make(map[int32]deviceController)
	for _, device := range devices {
		if device == nil {
			continue
		}
		if controller, ok := deviceControllerFor(device); ok {
			controllers[device.GetVirtualDevice().Key] = controller
		}
	}
	guestByKey := make(map[int32]types.GuestNicInfo)
	guestByMAC := make(map[string]types.GuestNicInfo)
	if guest != nil {
		for _, nic := range guest.Net {
			if nic.DeviceConfigId != 0 {
				guestByKey[nic.DeviceConfigId] = nic
			}
			if mac := normalizeMAC(nic.MacAddress); mac != "" {
				guestByMAC[mac] = nic
			}
		}
	}
	var disks []VMDisk
	var nics []VMNIC
	for _, device := range devices {
		if device == nil {
			continue
		}
		switch d := device.(type) {
		case *types.VirtualDisk:
			disks = append(disks, newVMDisk(d, controllers[d.ControllerKey]))
		case *types.VirtualEthernetCard:
			nics = append(nics, newVMNIC(d, "Ethernet", guestByKey, guestByMAC, idx))
		case *types.VirtualE1000, *types.VirtualE1000e, *types.VirtualPCNet32,
			*types.VirtualSriovEthernetCard, *types.VirtualVmxnet, *types.VirtualVmxnet2,
			*types.VirtualVmxnet3:
			// All concrete ethernet card types embed VirtualEthernetCard but do
			// not share a Go type, so normalize them through their base device.
			nics = append(nics, newVMNICFromDevice(device, guestByKey, guestByMAC, idx))
		}
	}
	sort.Slice(disks, func(i, j int) bool { return disks[i].Key < disks[j].Key })
	sort.Slice(nics, func(i, j int) bool { return nics[i].Key < nics[j].Key })
	return disks, nics
}

type deviceController struct {
	typeName, label, sharedBus string
	unitNumber                 *int32
}

func deviceControllerFor(device types.BaseVirtualDevice) (deviceController, bool) {
	base := device.GetVirtualDevice()
	label := ""
	if base.DeviceInfo != nil {
		label = base.DeviceInfo.GetDescription().Label
	}
	switch c := device.(type) {
	case *types.VirtualSCSIController:
		return deviceController{"SCSI", label, string(c.SharedBus), cloneInt32(c.ScsiCtlrUnitNumber)}, true
	case *types.VirtualBusLogicController:
		return deviceController{"BusLogic", label, "", cloneInt32(c.ScsiCtlrUnitNumber)}, true
	case *types.VirtualLsiLogicController:
		return deviceController{"LSI Logic", label, "", cloneInt32(c.ScsiCtlrUnitNumber)}, true
	case *types.VirtualLsiLogicSASController:
		return deviceController{"LSI Logic SAS", label, "", cloneInt32(c.ScsiCtlrUnitNumber)}, true
	case *types.VirtualIDEController:
		return deviceController{"IDE", label, "", nil}, true
	case *types.VirtualSATAController:
		return deviceController{"SATA", label, "", nil}, true
	case *types.VirtualAHCIController:
		return deviceController{"AHCI", label, "", nil}, true
	case *types.VirtualNVMEController:
		return deviceController{"NVMe", label, c.SharedBus, nil}, true
	default:
		return deviceController{}, false
	}
}

func newVMDisk(d *types.VirtualDisk, controller deviceController) VMDisk {
	out := VMDisk{Key: d.Key, Label: deviceLabel(d.DeviceInfo), CapacityBytes: d.CapacityInBytes, Controller: controller.typeName, ControllerLabel: controller.label, SharedBus: controller.sharedBus, UnitNumber: clonePtr(d.UnitNumber)}
	if out.CapacityBytes == 0 && d.CapacityInKB > 0 {
		out.CapacityBytes = d.CapacityInKB * 1024
	}
	if d.Shares != nil {
		out.Shares = cloneInt32(d.Shares.Shares)
		out.SharesLevel = string(d.Shares.Level)
	}
	if d.StorageIOAllocation != nil {
		out.Limit = clonePtr(d.StorageIOAllocation.Limit)
		out.Reservation = clonePtr(d.StorageIOAllocation.Reservation)
		if d.StorageIOAllocation.Shares != nil {
			out.Shares = cloneInt32(d.StorageIOAllocation.Shares.Shares)
			out.SharesLevel = string(d.StorageIOAllocation.Shares.Level)
		}
	}
	switch b := d.Backing.(type) {
	case *types.VirtualDiskFlatVer1BackingInfo:
		out.BackingType, out.BackingPath, out.DiskMode = "flatVer1", b.FileName, b.DiskMode
		out.Split, out.WriteThrough = clonePtr(b.Split), clonePtr(b.WriteThrough)
	case *types.VirtualDiskFlatVer2BackingInfo:
		out.BackingType, out.BackingPath, out.DiskMode, out.UUID, out.Sharing = "flatVer2", b.FileName, b.DiskMode, b.Uuid, b.Sharing
		out.ThinProvisioned, out.EagerlyScrub = clonePtr(b.ThinProvisioned), clonePtr(b.EagerlyScrub)
		out.Split, out.WriteThrough = clonePtr(b.Split), clonePtr(b.WriteThrough)
	case *types.VirtualDiskSparseVer1BackingInfo:
		out.BackingType, out.BackingPath, out.DiskMode = "sparseVer1", b.FileName, b.DiskMode
		out.Split, out.WriteThrough = clonePtr(b.Split), clonePtr(b.WriteThrough)
	case *types.VirtualDiskSparseVer2BackingInfo:
		out.BackingType, out.BackingPath, out.DiskMode, out.UUID = "sparseVer2", b.FileName, b.DiskMode, b.Uuid
		out.Split, out.WriteThrough = clonePtr(b.Split), clonePtr(b.WriteThrough)
	case *types.VirtualDiskSeSparseBackingInfo:
		out.BackingType, out.BackingPath, out.DiskMode, out.UUID = "seSparse", b.FileName, b.DiskMode, b.Uuid
		out.WriteThrough = clonePtr(b.WriteThrough)
	case *types.VirtualDiskRawDiskMappingVer1BackingInfo:
		out.BackingType, out.BackingPath, out.DiskMode, out.UUID = "rdm", b.FileName, b.DiskMode, b.Uuid
		out.Raw, out.RawLUNID, out.RawCompatibilityMode, out.Sharing = true, b.LunUuid, b.CompatibilityMode, b.Sharing
	case *types.VirtualDiskRawDiskVer2BackingInfo:
		out.BackingType, out.BackingPath, out.UUID, out.Sharing = "rawVer2", b.DescriptorFileName, b.Uuid, b.Sharing
		out.Raw = true
	case *types.VirtualDiskPartitionedRawDiskVer2BackingInfo:
		out.BackingType, out.BackingPath, out.UUID, out.Sharing = "rawPartitioned", b.DescriptorFileName, b.Uuid, b.Sharing
		out.Raw = true
	}
	if out.BackingPath == "" {
		if b, ok := d.Backing.(types.BaseVirtualDeviceFileBackingInfo); ok {
			out.BackingPath = b.GetVirtualDeviceFileBackingInfo().FileName
		}
	}
	return out
}

func newVMNICFromDevice(device types.BaseVirtualDevice, byKey map[int32]types.GuestNicInfo, byMAC map[string]types.GuestNicInfo, idx *index) VMNIC {
	switch d := device.(type) {
	case *types.VirtualE1000:
		return newVMNIC(&d.VirtualEthernetCard, "E1000", byKey, byMAC, idx)
	case *types.VirtualE1000e:
		return newVMNIC(&d.VirtualEthernetCard, "E1000e", byKey, byMAC, idx)
	case *types.VirtualPCNet32:
		return newVMNIC(&d.VirtualEthernetCard, "PCNet32", byKey, byMAC, idx)
	case *types.VirtualSriovEthernetCard:
		return newVMNIC(&d.VirtualEthernetCard, "SR-IOV", byKey, byMAC, idx)
	case *types.VirtualVmxnet:
		return newVMNIC(&d.VirtualEthernetCard, "Vmxnet", byKey, byMAC, idx)
	case *types.VirtualVmxnet2:
		return newVMNIC(&d.VirtualEthernetCard, "Vmxnet2", byKey, byMAC, idx)
	case *types.VirtualVmxnet3:
		return newVMNIC(&d.VirtualEthernetCard, "Vmxnet3", byKey, byMAC, idx)
	default:
		return VMNIC{}
	}
}

func newVMNIC(card *types.VirtualEthernetCard, adapter string, byKey map[int32]types.GuestNicInfo, byMAC map[string]types.GuestNicInfo, idx *index) VMNIC {
	out := VMNIC{Key: card.Key, Label: deviceLabel(card.DeviceInfo), Adapter: adapter, MACAddress: card.MacAddress, MACAddressType: card.AddressType}
	if card.Connectable != nil {
		out.Connected, out.StartsConnected = clonePtr(&card.Connectable.Connected), clonePtr(&card.Connectable.StartConnected)
	}
	if card.UptCompatibilityEnabled != nil {
		out.DirectPathIO = clonePtr(card.UptCompatibilityEnabled)
	}
	if card.MacAddress != "" {
		if nic, ok := byKey[card.Key]; ok {
			applyGuestNIC(&out, nic)
		} else if nic, ok := byMAC[normalizeMAC(card.MacAddress)]; ok {
			applyGuestNIC(&out, nic)
		}
	}
	if nic, ok := byKey[card.Key]; ok {
		applyGuestNIC(&out, nic)
	}
	applyNetworkBacking(&out, card.Backing, idx)
	return out
}

func applyGuestNIC(out *VMNIC, nic types.GuestNicInfo) {
	if nic.Network != "" {
		out.Network = nic.Network
	}
	if out.MACAddress == "" {
		out.MACAddress = nic.MacAddress
	}
	if out.Connected == nil {
		out.Connected = clonePtr(&nic.Connected)
	}
	for _, address := range nic.IpAddress {
		classifyAddress(out, address)
	}
	if nic.IpConfig != nil {
		for _, address := range nic.IpConfig.IpAddress {
			classifyAddress(out, address.IpAddress)
		}
	}
	sort.Strings(out.IPv4)
	sort.Strings(out.IPv6)
	out.IPv4 = uniqueStrings(out.IPv4)
	out.IPv6 = uniqueStrings(out.IPv6)
}

func applyNetworkBacking(out *VMNIC, backing types.BaseVirtualDeviceBackingInfo, idx *index) {
	switch b := backing.(type) {
	case *types.VirtualEthernetCardNetworkBackingInfo:
		out.NetworkID = refValue(b.Network)
		if out.Network == "" && b.DeviceName != "" {
			out.Network = b.DeviceName
		}
		if out.Network == "" && idx != nil {
			out.Network = idx.name(b.Network)
		}
	case *types.VirtualEthernetCardLegacyNetworkBackingInfo:
		if out.Network == "" {
			out.Network = b.DeviceName
		}
	case *types.VirtualEthernetCardDistributedVirtualPortBackingInfo:
		out.NetworkID, out.SwitchID = b.Port.PortgroupKey, b.Port.SwitchUuid
		if out.Network == "" && idx != nil && b.Port.PortgroupKey != "" {
			ref := types.ManagedObjectReference{Type: "DistributedVirtualPortgroup", Value: b.Port.PortgroupKey}
			out.Network = idx.name(&ref)
		}
	case *types.VirtualEthernetCardOpaqueNetworkBackingInfo:
		out.NetworkID = b.OpaqueNetworkId
		if out.Network == "" {
			out.Network = b.OpaqueNetworkId
		}
	}
}

func deviceLabel(info types.BaseDescription) string {
	if info == nil {
		return ""
	}
	return info.GetDescription().Label
}

func classifyAddress(out *VMNIC, address string) {
	ip := net.ParseIP(strings.TrimSpace(address))
	if ip == nil {
		return
	}
	if ip.To4() != nil {
		out.IPv4 = append(out.IPv4, ip.String())
		return
	}
	out.IPv6 = append(out.IPv6, ip.String())
}

func normalizeMAC(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(value), ":", ""), "-", ""))
}

func refValue(ref *types.ManagedObjectReference) string {
	if ref == nil {
		return ""
	}
	return ref.Value
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func clonePtr[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneInt32(value int32) *int32 { return &value }

func flattenSnapshots(tree []types.VirtualMachineSnapshotTree, parent string, current *types.ManagedObjectReference) []VMSnapshot {
	var out []VMSnapshot
	for i := range tree {
		n := tree[i]
		id := n.Snapshot.Value
		out = append(out, VMSnapshot{
			ID: id, NumericID: n.Id, ParentID: parent, Name: n.Name,
			Description: n.Description, CreateTime: n.CreateTime,
			PowerState: string(n.State), Quiesced: n.Quiesced,
			Current: current != nil && current.Value == id,
		})
		out = append(out, flattenSnapshots(n.ChildSnapshotList, id, current)...)
	}
	return out
}
