package rvimport

import (
	"reflect"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The "RVTools 4.8.2.1" cells are vcsa's in the real export of the nested
// lab; dates are entered as vsfleet writes them, in UTC.
func TestVInfoSettingsAndStateAreImported(t *testing.T) {
	columns := []string{"Config status", "PowerOn", "Suspend Interval", "Creation date", "Change Version", "Fixed Passthru HotPlug",
		"min Required EVC Mode Key", "Latency Sensitivity", "Op Notification Timeout", "EnableUUID", "CBT", "Num Monitors", "Video Ram KiB",
		"Folder ID", "Boot Required", "Boot delay", "Boot retry delay", "Boot retry enabled", "Boot BIOS setup", "Reboot PowerOff",
		"Customization Info", "Guest Detailed Data"}
	created := time.Date(2026, 9, 27, 15, 45, 42, 0, time.UTC)
	booted := time.Date(2026, 11, 2, 12, 25, 6, 0, time.UTC)
	no, zero, retry, monitors, ram := false, int64(0), int64(10000), int32(1), int64(4096)
	for _, tc := range []struct {
		name     string
		cells    []string
		folderID string
		settings *vsphere.VMSettings
		state    *vsphere.VMState
	}{
		{"RVTools 4.8.2.1", []string{"green", "2026/11/02 12:25:06", "0", "2026/09/27 15:45:42", "2026/11/02 12:25:03", "False", "intel-cascadelake",
			"normal", "", "", "", "1", "4096", "group-v11", "False", "0", "10000", "False", "False", "False", "IDLE", "distroName='VMware Photon OS'"},
			"group-v11",
			&vsphere.VMSettings{CreateDate: &created, LatencySensitivity: "normal", Monitors: &monitors, VideoRAMKiB: &ram, FixedPassthruHotPlug: &no,
				InstallBootRequired: &no, EnterBIOSSetup: &no, BootDelay: &zero, BootRetryEnabled: &no, BootRetryDelay: &retry},
			&vsphere.VMState{ChangeVersion: "2026-11-02T12:25:03Z", ConfigStatus: "green", BootTime: &booted, RebootPowerOff: &no, SuspendInterval: &zero,
				MinRequiredEVCModeKey: "intel-cascadelake", CustomizationStatus: "TOOLSDEPLOYPKG_IDLE", GuestDetailedData: "distroName='VMware Photon OS'"}},
		{"older workbook", make([]string, len(columns)), "", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openFixture(t, writeFixtureWorkbook(t, nil))
			row := vInfoRow(t, f, "web-01")
			for i, column := range columns {
				setColumnValue(t, f, sheetVInfo, column, row, tc.cells[i])
			}
			result, err := Parse(f, Options{})
			if err != nil {
				t.Fatal(err)
			}
			vm := findVM(result, "alpha", "web-01")
			if vm.FolderID != tc.folderID {
				t.Errorf("folder ID = %q, want %q", vm.FolderID, tc.folderID)
			}
			if !reflect.DeepEqual(vm.Settings, tc.settings) {
				t.Errorf("settings = %+v\nwant       %+v", vm.Settings, tc.settings)
			}
			if !reflect.DeepEqual(vm.State, tc.state) {
				t.Errorf("state = %+v\nwant    %+v", vm.State, tc.state)
			}
		})
	}
}

// RVTools names the column SMBIOS UUID; vsfleet wrote VM SMBIOS UUID before
// inventory schema 29, and both import. A clone keeps its BIOS UUID, so two
// VMs sharing one is not an ambiguity while each has its own VM ID.
func TestVInfoSMBIOSUUIDImportsUnderEitherNameWithoutJoiningClones(t *testing.T) {
	for _, header := range []string{"SMBIOS UUID", "VM SMBIOS UUID"} {
		t.Run(header, func(t *testing.T) {
			f := openFixture(t, writeFixtureWorkbook(t, func(data *assessment.ExportData) {
				data.VMs[0].Observation.VM.BIOSUUID = "4239ca73-e7ed-4ea6-5e1e-399e545c5b0c"
				clone := data.VMs[0]
				clone.Observation.VM.ID, clone.Observation.VM.InstanceUUID, clone.Observation.VM.Name = "vm-alpha-2", "uuid-clone", "web-02"
				clone.Snapshots = nil
				data.VMs = append(data.VMs, clone)
			}))
			setColumnValue(t, f, sheetVInfo, "SMBIOS UUID", 1, header)
			result, err := Parse(f, Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"web-01", "web-02"} {
				if vm := findVM(result, "alpha", name); vm == nil || vm.BIOSUUID != "4239ca73-e7ed-4ea6-5e1e-399e545c5b0c" {
					t.Errorf("%s = %+v, want the shared BIOS UUID", name, vm)
				}
			}
			if len(result.Report.Ambiguities) != 0 {
				t.Errorf("ambiguities = %+v, want none", result.Report.Ambiguities)
			}
		})
	}
}
