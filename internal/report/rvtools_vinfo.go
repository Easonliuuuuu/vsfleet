package report

import (
	"strings"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// vInfoDateHeaders are the vInfo columns written as date cells.
var vInfoDateHeaders = []string{"PowerOn", "Creation date", "Change Version"}

// headerPositions returns the 0-based positions of names in headers.
func headerPositions(headers []string, names ...string) []int {
	var out []int
	for i, h := range headers {
		for _, n := range names {
			if h == n {
				out = append(out, i)
			}
		}
	}
	return out
}

// vInfoCells holds the vInfo columns read from VM.Settings and VM.State, one
// group per run of adjacent RVTools columns. A VM captured before inventory
// schema 29, or imported from a workbook without the columns, leaves them
// all empty.
type vInfoCells struct {
	configStatus                                                      any
	powerOn, suspendInterval, created, changeVersion                  any
	fixedPassthru, minEVC, latency, opNotification, enableUUID, cbt   any
	monitors, videoRAM                                                any
	bootRequired, bootDelay, bootRetryDelay, bootRetry, bootBIOS, rpo any
	customization, guestData                                          any
}

func vmInfoCells(vm vsphere.VM) vInfoCells {
	var c vInfoCells
	if s := vm.Settings; s != nil {
		c.created = optionalTime(s.CreateDate)
		c.fixedPassthru, c.latency, c.opNotification = optionalBool(s.FixedPassthruHotPlug), optionalString(s.LatencySensitivity), optionalInt64(s.OpNotificationTimeout)
		c.enableUUID, c.cbt = optionalBool(s.EnableUUID), optionalBool(s.ChangeTracking)
		c.monitors, c.videoRAM = optionalInt32(s.Monitors), optionalInt64(s.VideoRAMKiB)
		c.bootRequired, c.bootDelay, c.bootRetryDelay = optionalBool(s.InstallBootRequired), optionalInt64(s.BootDelay), optionalInt64(s.BootRetryDelay)
		c.bootRetry, c.bootBIOS = optionalBool(s.BootRetryEnabled), optionalBool(s.EnterBIOSSetup)
	}
	if s := vm.State; s != nil {
		c.configStatus, c.powerOn, c.suspendInterval = optionalString(s.ConfigStatus), optionalTime(s.BootTime), optionalInt64(s.SuspendInterval)
		c.changeVersion, c.minEVC, c.rpo = changeVersionCell(s.ChangeVersion), optionalString(s.MinRequiredEVCModeKey), optionalBool(s.RebootPowerOff)
		// RVTools drops the TOOLSDEPLOYPKG_ prefix vSphere puts on every status.
		c.customization = optionalString(strings.TrimPrefix(s.CustomizationStatus, "TOOLSDEPLOYPKG_"))
		c.guestData = optionalString(s.GuestDetailedData)
	}
	return c
}

// changeVersionCell writes vSphere's change version, a timestamp, as a date
// the way RVTools does; a value that is not a timestamp is written as text.
func changeVersionCell(v string) any {
	if v == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t.UTC()
	}
	return v
}

// sdkSource writes vInfo's VI SDK Server type and VI SDK API Version: the
// vCenter's full product name and API version, empty when the run recorded
// no source for the context.
func sdkSource(data assessment.ExportData, context string) (any, any) {
	for _, c := range data.Contexts {
		if c.Name == context && c.Source != nil {
			return optionalString(c.Source.FullName), optionalString(c.Source.APIVersion)
		}
	}
	return nil, nil
}
