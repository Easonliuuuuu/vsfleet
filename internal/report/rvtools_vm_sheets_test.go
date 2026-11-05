package report

import (
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The values are vcsa's and vm-snapshot's in the real RVTools 4.8.2.1 export
// of the nested lab, where the snapshot sizes print with a last digit of 2
// and 9 for the same 9651 and 475 bytes. A capture predating inventory schema 32 leaves every new
// cell blank rather than writing false or zero.
func TestWriteRVToolsVMSheetConfigurationColumns(t *testing.T) {
	yes, no := true, false
	cpuShares, memShares, bus := int32(2000), int32(143360), int32(0)
	vmsn, total := int64(9651), int64(475)
	lab := map[string]map[string]string{
		"vCPU":    {"Level": "normal", "Shares": "2000", "Hot Add": "TRUE", "Hot Remove": "TRUE", "Numa Hotadd Exposed": "FALSE"},
		"vMemory": {"Level": "normal", "Shares": "143360", "Hot Add": "TRUE"},
		"vTools": {"VM Version": "10", "Required Version": "12448", "Upgrade Policy": "manual", "Sync time": "TRUE", "Kernel Crash state": "FALSE",
			"Operation Ready": "TRUE", "State change support": "TRUE", "Interactive Guest": "FALSE"},
		"vUSB":      {"EHCI enabled": "TRUE", "Auto connect": "FALSE", "Bus number": "0"},
		"vDisk":     {"Disk Path": "/, /boot"},
		"vSnapshot": {"Filename": "[nfs-shared] vm-snapshot/vm-snapshot-Snapshot1.vmsn", "Size MiB (vmsn)": "0.00920391082763671", "Size MiB (total)": "0.000452995300292968"},
	}
	for _, tc := range []struct {
		name   string
		mutate func(vm *vsphere.VM, snaps []vsphere.VMSnapshot)
		want   func(sheet, header string) string
	}{
		{"lab", func(vm *vsphere.VM, snaps []vsphere.VMSnapshot) {
			vm.HardwareVersion = "vmx-10"
			vm.Settings = &vsphere.VMSettings{CPUHotAdd: &yes, CPUHotRemove: &yes, VNUMAOnCPUHotAdd: &no, MemoryHotAdd: &yes, CPUShares: &cpuShares, CPUSharesLevel: "normal",
				MemoryShares: &memShares, MemorySharesLevel: "normal", ToolsUpgradePolicy: "manual", ToolsSyncTime: &yes}
			vm.State = &vsphere.VMState{ToolsRequiredVersion: "12448", GuestKernelCrashed: &no, GuestOperationsReady: &yes, InteractiveGuestOperationsReady: &no, GuestStateChangeSupported: &yes}
			vm.USBs[0].EHCIEnabled, vm.USBs[0].AutoConnect, vm.USBs[0].BusNumber = &yes, &no, &bus
			// vcsa's /boot is on the same disk as /; /storage/log is on another.
			vm.Partitions = []vsphere.VMPartition{{Path: "/", DiskKeys: []int32{101}}, {Path: "/boot", DiskKeys: []int32{101}}, {Path: "/storage/log", DiskKeys: []int32{102}}}
			snaps[0].Filename, snaps[0].DataBytes, snaps[0].TotalBytes = "[nfs-shared] vm-snapshot/vm-snapshot-Snapshot1.vmsn", &vmsn, &total
		}, func(sheet, header string) string { return lab[sheet][header] }},
		{"captured before schema 32", func(*vsphere.VM, []vsphere.VMSnapshot) {}, func(string, string) string { return "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			tc.mutate(&data.VMs[0].Observation.VM, data.VMs[0].Snapshots)
			for sheet, cells := range lab {
				got := firstRowByHeader(t, data, sheet)
				for header := range cells {
					if cell, found := got[header]; !found || cell != tc.want(sheet, header) {
						t.Errorf("%s/%s = %q (column present: %v), want %q", sheet, header, cell, found, tc.want(sheet, header))
					}
				}
			}
		})
	}
}

// The vNetwork Switch is the switch of the NIC's port group, a distributed
// one by the UUID its backing names and a standard one through the VM host's
// port groups. A port group the capture has no switch for is blank.
func TestWriteRVToolsNamesEachNICsSwitch(t *testing.T) {
	for _, tc := range []struct {
		name, network, switchID, want string
	}{
		{"standard port group", "Management Network", "", "vSwitch0"},
		{"distributed port group", "frontend", "dvs-uuid", "DVS-1"},
		{"port group the capture has no switch for", "VM Network", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			nic := &data.VMs[0].Observation.VM.NICs[0]
			nic.Network, nic.SwitchID, nic.MACAddressType = tc.network, tc.switchID, "assigned"
			got := firstRowByHeader(t, data, "vNetwork")
			if got["Switch"] != tc.want || got["Type"] != "assigned" {
				t.Errorf("Switch, Type = %q, %q, want %q, assigned", got["Switch"], got["Type"], tc.want)
			}
		})
	}
}
