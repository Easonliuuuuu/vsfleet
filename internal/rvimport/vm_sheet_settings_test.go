package rvimport

import (
	"reflect"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The vCPU, vMemory, vTools, vUSB, vNetwork and vSnapshot configuration a
// vsfleet export writes imports back as it was captured. The values are the
// nested lab's vcsa and vm-snapshot.
func TestVMSheetConfigurationRoundTrips(t *testing.T) {
	yes, no := true, false
	cpuShares, memShares, bus := int32(2000), int32(143360), int32(0)
	vmsn, total := int64(9651), int64(475)
	settings := vsphere.VMSettings{CPUHotAdd: &yes, CPUHotRemove: &yes, VNUMAOnCPUHotAdd: &no, MemoryHotAdd: &yes, CPUShares: &cpuShares, CPUSharesLevel: "normal",
		MemoryShares: &memShares, MemorySharesLevel: "normal", ToolsUpgradePolicy: "manual", ToolsSyncTime: &yes}
	state := vsphere.VMState{ToolsRequiredVersion: "12448", GuestKernelCrashed: &no, GuestOperationsReady: &yes, InteractiveGuestOperationsReady: &no, GuestStateChangeSupported: &yes}
	created := time.Date(2026, 9, 27, 16, 29, 35, 0, time.UTC)
	f := openFixture(t, writeFixtureWorkbook(t, func(data *assessment.ExportData) {
		vm := &data.VMs[0].Observation.VM
		vm.HardwareVersion = "vmx-10"
		vm.Settings, vm.State = &settings, &state
		vm.NICs[0].MACAddressType = "assigned"
		vm.USBs = []vsphere.VMUSB{{Key: 4000, Label: "USB device 1", EHCIEnabled: &yes, AutoConnect: &no, BusNumber: &bus}}
		data.VMs[0].Snapshots = []vsphere.VMSnapshot{{ID: "snapshot-1017", Name: "snap-1", CreateTime: created,
			Filename: "[nfs-shared] vm-snapshot/vm-snapshot-Snapshot1.vmsn", DataBytes: &vmsn, TotalBytes: &total}}
	}))
	result, err := Parse(f, Options{})
	if err != nil {
		t.Fatal(err)
	}
	vm := findVM(result, "alpha", "web-01")
	if vm.Settings == nil || !reflect.DeepEqual(*vm.Settings, settings) {
		t.Errorf("settings = %+v, want %+v", vm.Settings, settings)
	}
	if vm.State == nil || !reflect.DeepEqual(*vm.State, state) {
		t.Errorf("state = %+v, want %+v", vm.State, state)
	}
	if vm.HardwareVersion != "vmx-10" {
		t.Errorf("hardware version = %q, want vmx-10", vm.HardwareVersion)
	}
	if len(vm.NICs) != 1 || vm.NICs[0].MACAddressType != "assigned" {
		t.Errorf("NICs = %+v, want MAC address type assigned", vm.NICs)
	}
	if len(vm.USBs) != 1 || !reflect.DeepEqual([]any{vm.USBs[0].EHCIEnabled, vm.USBs[0].AutoConnect, vm.USBs[0].BusNumber}, []any{&yes, &no, &bus}) {
		t.Errorf("USBs = %+v, want EHCI on, auto connect off, bus 0", vm.USBs)
	}
	if len(vm.Snapshots) != 1 {
		t.Fatalf("snapshots = %+v", vm.Snapshots)
	}
	if s := vm.Snapshots[0]; s.Filename != "[nfs-shared] vm-snapshot/vm-snapshot-Snapshot1.vmsn" || s.DataBytes == nil || *s.DataBytes != vmsn || s.TotalBytes == nil || *s.TotalBytes != total {
		t.Errorf("snapshot files = %q, %v, %v, want its file, 9651 and 475 bytes", s.Filename, s.DataBytes, s.TotalBytes)
	}
}

// vsfleet wrote vNetwork's Type as Mac Address type before inventory schema 32.
func TestVNetworkTypeImportsUnderEitherName(t *testing.T) {
	for _, header := range []string{"Type", "Mac Address type"} {
		t.Run(header, func(t *testing.T) {
			f := openFixture(t, writeFixtureWorkbook(t, func(data *assessment.ExportData) {
				data.VMs[0].Observation.VM.NICs[0].MACAddressType = "manual"
			}))
			setColumnValue(t, f, sheetVNetwork, "Type", 1, header)
			result, err := Parse(f, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if vm := findVM(result, "alpha", "web-01"); len(vm.NICs) != 1 || vm.NICs[0].MACAddressType != "manual" {
				t.Errorf("NICs = %+v, want MAC address type manual", vm.NICs)
			}
		})
	}
}
