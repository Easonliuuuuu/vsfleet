package report

import (
	"encoding/json"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// vmSettingsOf and vmStateOf return the VM's settings and state, empty for a
// capture predating inventory schema 29, whose cells stay blank.
func vmSettingsOf(vm vsphere.VM) vsphere.VMSettings {
	if vm.Settings == nil {
		return vsphere.VMSettings{}
	}
	return *vm.Settings
}

func vmStateOf(vm vsphere.VM) vsphere.VMState {
	if vm.State == nil {
		return vsphere.VMState{}
	}
	return *vm.State
}

// switchKey names a switch's port group: a distributed switch by its UUID,
// a standard one by its host and port group name.
type switchKey struct{ context, scope, name string }

// vmNICSwitches maps each port group a NIC can be on to its switch's name,
// from the host and distributed switch configuration the capture recorded.
func vmNICSwitches(data assessment.ExportData) map[switchKey]string {
	out := make(map[switchKey]string)
	for _, item := range hostConfigResources(data) {
		for _, port := range item.host.PortGroups {
			out[switchKey{item.resource.Context, item.host.Name, port.Name}] = port.Switch
		}
	}
	for _, resource := range data.Resources {
		if resource.Kind != "dvswitch" {
			continue
		}
		var sw vsphere.DVSwitch
		if err := json.Unmarshal(resource.Payload, &sw); err == nil && sw.UUID != "" {
			out[switchKey{resource.Context, "dvs", sw.UUID}] = nonempty(sw.Name, resource.Name)
		}
	}
	return out
}

// nicSwitchCell is the switch a NIC's port group belongs to; blank when the
// capture recorded no such switch.
func nicSwitchCell(switches map[switchKey]string, context string, vm vsphere.VM, nic vsphere.VMNIC) any {
	key := switchKey{context, vm.Host, nic.Network}
	if nic.SwitchID != "" {
		key = switchKey{context, "dvs", nic.SwitchID}
	}
	return optionalString(switches[key])
}

// diskPathCell lists the guest filesystems VMware Tools maps to the disk,
// in the order vPartition lists them; blank without a mapping.
func diskPathCell(vm vsphere.VM, key int32) any {
	var paths []string
	for _, p := range vm.Partitions {
		for _, k := range p.DiskKeys {
			if k == key {
				paths = append(paths, p.Path)
				break
			}
		}
	}
	return optionalString(strings.Join(paths, ", "))
}
