package report

import (
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The "lab" values are vcsa's and vm-iso's in the real RVTools 4.8.2.1
// export of the nested lab. RVTools repeats SRM Placeholder and the
// Tools-reported OS on most VM sheets, and spells the OS column with a
// lower-case t on vUSB.
func TestWriteRVToolsRepeatsSRMPlaceholderAndToolsOS(t *testing.T) {
	no := false
	sheets := []struct {
		name, osHeader string
		srm            bool
	}{
		{"vInfo", osToolsHeader, true}, {"vCPU", osToolsHeader, true}, {"vMemory", osToolsHeader, true},
		{"vTools", osToolsHeader, true}, {"vDisk", osToolsHeader, true}, {"vNetwork", osToolsHeader, true},
		{"vPartition", osToolsHeader, true}, {"vCD", osToolsHeader, true}, {"vUSB", usbOSToolsHeader, true},
		{"vSnapshot", osToolsHeader, false},
	}
	for _, tc := range []struct {
		name    string
		mutate  func(*vsphere.VM)
		srm, os string
		cdType  string
		usbType string
	}{
		{"lab", func(vm *vsphere.VM) {
			vm.Settings = &vsphere.VMSettings{SRMPlaceholder: &no}
			vm.State = &vsphere.VMState{GuestOSReported: "VMware Photon OS (64-bit)"}
			vm.CDROMs[0].Summary = "ISO [nfs-shared] ISO/test.iso"
			vm.USBs[0].Summary = "Remote host esx-1"
		}, "FALSE", "VMware Photon OS (64-bit)", "ISO [nfs-shared] ISO/test.iso", "Remote host esx-1"},
		{"captured before schema 31", func(*vsphere.VM) {}, "", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			tc.mutate(&data.VMs[0].Observation.VM)
			for _, sh := range sheets {
				got := firstRowByHeader(t, data, sh.name)
				if cell, found := got[sh.osHeader]; !found || cell != tc.os {
					t.Errorf("%s/%s = %q (column present: %v), want %q", sh.name, sh.osHeader, cell, found, tc.os)
				}
				if cell, found := got["SRM Placeholder"]; found != sh.srm || cell != map[bool]string{true: tc.srm}[sh.srm] {
					t.Errorf("%s/SRM Placeholder = %q (column present: %v)", sh.name, cell, found)
				}
			}
			if got := firstRowByHeader(t, data, "vCD"); got["Device Node"] != "CD/DVD drive 1" || got["Device Type"] != tc.cdType {
				t.Errorf("vCD Device Node, Device Type = %q, %q, want CD/DVD drive 1, %q", got["Device Node"], got["Device Type"], tc.cdType)
			}
			if got := firstRowByHeader(t, data, "vUSB"); got["Device Node"] != "USB device 1" || got["Device Type"] != tc.usbType {
				t.Errorf("vUSB Device Node, Device Type = %q, %q, want USB device 1, %q", got["Device Node"], got["Device Type"], tc.usbType)
			}
			if _, found := firstRowByHeader(t, data, "vUSB")[osToolsHeader]; found {
				t.Errorf("vUSB has %q, want RVTools' %q", osToolsHeader, usbOSToolsHeader)
			}
		})
	}
}
