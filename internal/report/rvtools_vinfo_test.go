package report

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The "recorded" row is the nested lab's vcsa as vCenter 8.0.3 reports it and
// the real RVTools 4.8.2.1 export writes it, apart from the dates, which
// vsfleet writes in UTC and RVTools in the collector's local time. The VI SDK
// columns come from the run's synthetic source record.
func TestWriteRVToolsVInfoSettingsAndState(t *testing.T) {
	columns := []string{"Config status", "PowerOn", "Suspend Interval", "Creation date", "Change Version", "Fixed Passthru HotPlug",
		"min Required EVC Mode Key", "Latency Sensitivity", "Op Notification Timeout", "EnableUUID", "CBT", "Num Monitors", "Video Ram KiB",
		"Folder ID", "Boot Required", "Boot delay", "Boot retry delay", "Boot retry enabled", "Boot BIOS setup", "Reboot PowerOff",
		"Customization Info", "Guest Detailed Data", "SMBIOS UUID", "VI SDK Server type", "VI SDK API Version"}
	created := time.Date(2026, 9, 27, 15, 45, 42, 0, time.UTC)
	booted := time.Date(2026, 11, 2, 12, 25, 6, 0, time.UTC)
	no, zero, retry, monitors, ram := false, int64(0), int64(10000), int32(1), int64(4096)
	for _, tc := range []struct {
		name string
		vm   func(*vsphere.VM)
		want []string
	}{
		{"recorded", func(vm *vsphere.VM) {
			vm.Folder, vm.FolderID = "/vCLS", "group-v11"
			vm.Settings = &vsphere.VMSettings{CreateDate: &created, ChangeTracking: &no, LatencySensitivity: "normal", Monitors: &monitors,
				VideoRAMKiB: &ram, FixedPassthruHotPlug: &no, InstallBootRequired: &no, EnterBIOSSetup: &no, BootDelay: &zero,
				BootRetryEnabled: &no, BootRetryDelay: &retry}
			vm.State = &vsphere.VMState{ChangeVersion: "2026-11-02T12:25:03Z", ConfigStatus: "green", BootTime: &booted, RebootPowerOff: &no,
				SuspendInterval: &zero, MinRequiredEVCModeKey: "intel-cascadelake", CustomizationStatus: "TOOLSDEPLOYPKG_IDLE",
				GuestDetailedData: "distroName='VMware Photon OS'"}
		}, []string{"green", "2026/11/02 12:25:06", "0", "2026/09/27 15:45:42", "2026/11/02 12:25:03", "FALSE", "intel-cascadelake", "normal", "",
			"", "FALSE", "1", "4096", "group-v11", "FALSE", "0", "10000", "FALSE", "FALSE", "FALSE", "IDLE", "distroName='VMware Photon OS'",
			"bios", "VMware vCenter Server 8.0.3 build-1000001", "8.0.3.0"}},
		{"captured before schema 29", func(*vsphere.VM) {}, []string{"", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "",
			"", "", "", "bios", "VMware vCenter Server 8.0.3 build-1000001", "8.0.3.0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			tc.vm(&data.VMs[0].Observation.VM)
			var buf bytes.Buffer
			if err := WriteRVTools(&buf, data, healthReport(data)); err != nil {
				t.Fatal(err)
			}
			f, err := excelize.OpenReader(&buf)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			got := sheetCells(t, f, "vInfo", "app", columns)
			for i, column := range columns {
				if got[i] != tc.want[i] {
					t.Errorf("%s = %q, want %q", column, got[i], tc.want[i])
				}
			}
		})
	}
}
