package rvimport

import (
	"strings"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// applyVMSettingsCells reads vInfo's folder ID, boot, hardware and identity
// settings and the power and guest state columns. Settings and State stay
// nil when the row carries none of them, as an older workbook does, so the VM
// reads as "not read". Every cell is read first so the column report sees
// each one.
func applyVMSettingsCells(t sheetTable, row []string, vm *vsphere.VM, timezone *time.Location) {
	vm.FolderID = t.cell(row, "Folder ID")
	s := vsphere.VMSettings{
		CreateDate:            optionalTime(t.cell(row, "Creation date"), timezone),
		EnableUUID:            parseOptBool(t.cell(row, "EnableUUID")),
		ChangeTracking:        parseOptBool(t.cell(row, "CBT")),
		LatencySensitivity:    t.cell(row, "Latency Sensitivity"),
		Monitors:              parseOptInt32(t.cell(row, "Num Monitors")),
		VideoRAMKiB:           parseOptInt64(t.cell(row, "Video Ram KiB")),
		FixedPassthruHotPlug:  parseOptBool(t.cell(row, "Fixed Passthru HotPlug")),
		OpNotificationTimeout: parseOptInt64(t.cell(row, "Op Notification Timeout")),
		InstallBootRequired:   parseOptBool(t.cell(row, "Boot Required")),
		EnterBIOSSetup:        parseOptBool(t.cell(row, "Boot BIOS setup")),
		BootDelay:             parseOptInt64(t.cell(row, "Boot delay")),
		BootRetryEnabled:      parseOptBool(t.cell(row, "Boot retry enabled")),
		BootRetryDelay:        parseOptInt64(t.cell(row, "Boot retry delay")),
	}
	st := vsphere.VMState{
		ConfigStatus:          t.cell(row, "Config status"),
		BootTime:              optionalTime(t.cell(row, "PowerOn"), timezone),
		RebootPowerOff:        parseOptBool(t.cell(row, "Reboot PowerOff")),
		SuspendInterval:       parseOptInt64(t.cell(row, "Suspend Interval")),
		MinRequiredEVCModeKey: t.cell(row, "min Required EVC Mode Key"),
		GuestDetailedData:     t.cell(row, "Guest Detailed Data"),
	}
	if c := t.cell(row, "Customization Info"); c != "" {
		st.CustomizationStatus = "TOOLSDEPLOYPKG_" + strings.TrimPrefix(c, "TOOLSDEPLOYPKG_")
	}
	if v := optionalTime(t.cell(row, "Change Version"), timezone); v != nil {
		st.ChangeVersion = v.Format(time.RFC3339Nano)
	}
	if s != (vsphere.VMSettings{}) {
		vm.Settings = &s
	}
	if st != (vsphere.VMState{}) {
		vm.State = &st
	}
}
