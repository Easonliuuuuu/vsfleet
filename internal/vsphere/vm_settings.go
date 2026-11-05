package vsphere

import (
	"strconv"
	"strings"
	"time"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// VMSettings is the boot, hardware and identity configuration RVTools
// reports on vInfo. It is nil for a capture predating inventory schema 29
// and for a listing, which does not read it. A nil field inside it was not
// reported: vCenter older than the property, or a value the VM does not set.
type VMSettings struct {
	CreateDate *time.Time `json:"create_date,omitempty"`
	// EnableUUID is the disk.EnableUUID advanced setting, which lets the
	// guest see stable disk serial numbers. It is read on vCenter 8.0 and
	// later only, and nil when the VM does not set it.
	EnableUUID           *bool  `json:"enable_uuid,omitempty"`
	ChangeTracking       *bool  `json:"change_tracking,omitempty"`
	LatencySensitivity   string `json:"latency_sensitivity,omitempty"`
	Monitors             *int32 `json:"monitors,omitempty"`
	VideoRAMKiB          *int64 `json:"video_ram_kib,omitempty"`
	FixedPassthruHotPlug *bool  `json:"fixed_passthru_hot_plug,omitempty"`
	// OpNotificationTimeout is how long, in seconds, a guest application
	// may take to prepare once notified of a host operation; nil when the VM
	// uses the host or cluster default.
	OpNotificationTimeout *int64 `json:"op_notification_timeout,omitempty"`
	InstallBootRequired   *bool  `json:"install_boot_required,omitempty"`
	EnterBIOSSetup        *bool  `json:"enter_bios_setup,omitempty"`
	// BootDelay and BootRetryDelay are in milliseconds.
	BootDelay        *int64 `json:"boot_delay,omitempty"`
	BootRetryEnabled *bool  `json:"boot_retry_enabled,omitempty"`
	BootRetryDelay   *int64 `json:"boot_retry_delay,omitempty"`
	// SRMPlaceholder reports a placeholder VM Site Recovery Manager keeps at
	// the recovery site for a protected VM; nil before inventory schema 31.
	SRMPlaceholder *bool `json:"srm_placeholder,omitempty"`
	// The hot-plug, shares and Tools settings below are nil or empty before
	// inventory schema 32. VNUMAOnCPUHotAdd is read on vCenter 8.0 Update 1
	// and later.
	CPUHotAdd         *bool  `json:"cpu_hot_add,omitempty"`
	CPUHotRemove      *bool  `json:"cpu_hot_remove,omitempty"`
	VNUMAOnCPUHotAdd  *bool  `json:"vnuma_on_cpu_hot_add,omitempty"`
	MemoryHotAdd      *bool  `json:"memory_hot_add,omitempty"`
	CPUShares         *int32 `json:"cpu_shares,omitempty"`
	CPUSharesLevel    string `json:"cpu_shares_level,omitempty"`
	MemoryShares      *int32 `json:"memory_shares,omitempty"`
	MemorySharesLevel string `json:"memory_shares_level,omitempty"`
	// ToolsUpgradePolicy is manual or upgradeAtPowerCycle.
	ToolsUpgradePolicy string `json:"tools_upgrade_policy,omitempty"`
	ToolsSyncTime      *bool  `json:"tools_sync_time,omitempty"`
}

// VMState is what changes with the VM's life rather than its configuration:
// the last power-on, the change version vCenter bumps on every
// reconfiguration, and what the guest and vCenter currently report. Diffs
// compare it only on request, like power state.
type VMState struct {
	ChangeVersion  string     `json:"change_version,omitempty"`
	ConfigStatus   string     `json:"config_status,omitempty"`
	BootTime       *time.Time `json:"boot_time,omitempty"`
	RebootPowerOff *bool      `json:"reboot_power_off,omitempty"`
	// SuspendInterval is how long the VM has been suspended, in seconds.
	SuspendInterval       *int64 `json:"suspend_interval,omitempty"`
	MinRequiredEVCModeKey string `json:"min_required_evc_mode_key,omitempty"`
	CustomizationStatus   string `json:"customization_status,omitempty"`
	GuestDetailedData     string `json:"guest_detailed_data,omitempty"`
	// GuestOSReported is the guest OS VMware Tools reports, which can differ
	// from the one the configuration names (VM.GuestOS).
	GuestOSReported string `json:"guest_os_reported,omitempty"`
	// The guest operation states VMware Tools reports; nil before inventory
	// schema 32 and for a template, which has no guest.
	GuestKernelCrashed              *bool `json:"guest_kernel_crashed,omitempty"`
	GuestOperationsReady            *bool `json:"guest_operations_ready,omitempty"`
	InteractiveGuestOperationsReady *bool `json:"interactive_guest_operations_ready,omitempty"`
	GuestStateChangeSupported       *bool `json:"guest_state_change_supported,omitempty"`
	// ToolsRequiredVersion is the VMware Tools version the VM's host
	// bundles, so it changes when the host is upgraded or the VM moves. It is
	// read on vCenter 8.0 and later.
	ToolsRequiredVersion string `json:"tools_required_version,omitempty"`
}

// The vCenter extension and managed-entity type Site Recovery Manager marks
// its placeholder VMs with.
const (
	srmExtensionKey    = "com.vmware.vcDr"
	srmPlaceholderType = "placeholderVm"
)

// vmSettingsProps are read with the full property set on every vCenter.
var vmSettingsProps = []string{
	"config.createDate",
	"config.changeVersion",
	"config.changeTrackingEnabled",
	"config.latencySensitivity",
	"config.vAppConfig.installBootRequired",
	"runtime.bootTime",
	"runtime.suspendInterval",
	"runtime.minRequiredEVCModeKey",
	"configStatus",
	"guest.guestFullName",
}

// versionedVMProps are properties a vCenter older than their API version
// does not have. Asking one for them fails the whole retrieval, so they are
// requested only from a vCenter that reports that version or later. The
// extraConfig key selector reads one advanced setting rather than all of
// them, which can carry guestinfo payloads; it is verified on 8.0.3.
var versionedVMProps = []struct {
	since string
	path  string
}{
	{"7.0.2.0", "guest.customizationInfo"},
	{"8.0.0.0", `config.extraConfig["disk.EnableUUID"]`},
	{"8.0.0.0", `config.extraConfig["` + toolsRequiredVersionKey + `"]`},
	{"8.0.0.1", "config.numaInfo"},
	{"8.0.0.1", "config.rebootPowerOff"},
	{"8.0.0.1", "config.vmOpNotificationTimeout"},
	{"8.0.1.0", "config.fixedPassthruHotPlugEnabled"},
	{"8.0.2.0", "guest.guestDetailedData"},
}

// propsForAPI returns the VM properties one detail level needs from a
// vCenter that speaks apiVersion.
func propsForAPI(detail Detail, apiVersion string) []string {
	props := propsFor(detail)
	if detail == DetailSummary {
		return props
	}
	out := append(append(append([]string{}, props...), vmSettingsProps...), vmSheetSettingsProps...)
	for _, p := range versionedVMProps {
		if apiAtLeast(apiVersion, p.since) {
			out = append(out, p.path)
		}
	}
	return out
}

// apiAtLeast compares dot-separated vSphere API versions numerically. An
// unparseable version is treated as older than every version.
func apiAtLeast(version, minimum string) bool {
	have, want := strings.Split(version, "."), strings.Split(minimum, ".")
	for i := range want {
		w, err := strconv.Atoi(want[i])
		if err != nil {
			return false
		}
		h := 0
		if i < len(have) {
			if h, err = strconv.Atoi(have[i]); err != nil {
				return false
			}
		}
		if h != w {
			return h > w
		}
	}
	return true
}

// vmSettings copies the vInfo configuration of a VM read with the full
// property set from a vCenter that speaks apiVersion; nil when vCenter
// returned no configuration.
func vmSettings(m *mo.VirtualMachine, apiVersion string) *VMSettings {
	cfg := m.Config
	if cfg == nil {
		return nil
	}
	s := &VMSettings{
		CreateDate:           clonePtr(cfg.CreateDate),
		ChangeTracking:       clonePtr(cfg.ChangeTrackingEnabled),
		FixedPassthruHotPlug: clonePtr(cfg.FixedPassthruHotPlugEnabled),
	}
	if l := cfg.LatencySensitivity; l != nil {
		s.LatencySensitivity = string(l.Level)
	}
	// vSphere reports -1 for a VM that uses the host or cluster default, and
	// an older vCenter, which was not asked, leaves the zero value.
	if t := cfg.VmOpNotificationTimeout; t >= 0 && apiAtLeast(apiVersion, "8.0.0.1") {
		s.OpNotificationTimeout = &t
	}
	// A VM without vApp options has no install boot to require.
	installBoot := false
	if v := cfg.VAppConfig; v != nil {
		installBoot = v.GetVmConfigInfo().InstallBootRequired
	}
	s.InstallBootRequired = &installBoot
	placeholder := cfg.ManagedBy != nil && cfg.ManagedBy.ExtensionKey == srmExtensionKey && cfg.ManagedBy.Type == srmPlaceholderType
	s.SRMPlaceholder = &placeholder
	if b := cfg.BootOptions; b != nil {
		delay, retryDelay := b.BootDelay, b.BootRetryDelay
		s.EnterBIOSSetup, s.BootDelay, s.BootRetryEnabled, s.BootRetryDelay = clonePtr(b.EnterBIOSSetup), &delay, clonePtr(b.BootRetryEnabled), &retryDelay
	}
	for _, o := range cfg.ExtraConfig {
		if v := o.GetOptionValue(); v != nil && strings.EqualFold(v.Key, "disk.EnableUUID") {
			if s2, ok := v.Value.(string); ok {
				if enabled, err := strconv.ParseBool(strings.ToLower(strings.TrimSpace(s2))); err == nil {
					s.EnableUUID = &enabled
				}
			}
		}
	}
	for _, d := range cfg.Hardware.Device {
		if video, ok := d.(*types.VirtualMachineVideoCard); ok {
			monitors, ram := video.NumDisplays, video.VideoRamSizeInKB
			s.Monitors, s.VideoRAMKiB = &monitors, &ram
			break
		}
	}
	applySheetSettings(s, cfg, apiVersion)
	return s
}

// vmState copies the vInfo state of a VM read with the full property set.
func vmState(m *mo.VirtualMachine) *VMState {
	interval := m.Runtime.SuspendInterval
	s := &VMState{
		ConfigStatus:          string(m.ConfigStatus),
		BootTime:              clonePtr(m.Runtime.BootTime),
		SuspendInterval:       &interval,
		MinRequiredEVCModeKey: m.Runtime.MinRequiredEVCModeKey,
	}
	if cfg := m.Config; cfg != nil {
		s.ChangeVersion = strings.TrimSpace(cfg.ChangeVersion)
		s.RebootPowerOff = clonePtr(cfg.RebootPowerOff)
		s.ToolsRequiredVersion = extraConfigString(cfg.ExtraConfig, toolsRequiredVersionKey)
	}
	if g := m.Guest; g != nil {
		s.GuestDetailedData = strings.TrimSpace(g.GuestDetailedData)
		s.GuestOSReported = strings.TrimSpace(g.GuestFullName)
		if c := g.CustomizationInfo; c != nil {
			s.CustomizationStatus = c.CustomizationStatus
		}
		applySheetState(s, g)
	}
	return s
}
