package vsphere

import (
	"strings"

	"github.com/vmware/govmomi/vim25/types"
)

// HostMultipathLUN is what the host reports about a multipathed LUN and its
// paths, the vMultiPath columns of an RVTools export. Vendor, Model and
// Serial keep the SCSI inquiry's space padding, as vSphere and RVTools
// report them.
type HostMultipathLUN struct {
	// CanonicalName is the device name the host's datastores name their
	// extents by, e.g. naa.600508b1001c4d41 or t10.ATA_….
	CanonicalName string `json:"canonical_name,omitempty"`
	// OperationalState is the LUN's state, e.g. ok, or several joined by
	// ", ".
	OperationalState string `json:"operational_state,omitempty"`
	// VStorageSupport is the hardware acceleration status, e.g.
	// vStorageSupported or vStorageUnsupported.
	VStorageSupport string `json:"vstorage_support,omitempty"`
	QueueDepth      *int32 `json:"queue_depth,omitempty"`
	Vendor          string `json:"vendor,omitempty"`
	Model           string `json:"model,omitempty"`
	Revision        string `json:"revision,omitempty"`
	SCSILevel       *int32 `json:"scsi_level,omitempty"`
	Serial          string `json:"serial,omitempty"`
	UUID            string `json:"uuid,omitempty"`
	// Paths are the LUN's paths in the order the host lists them.
	Paths []HostMultipathPath `json:"paths,omitempty"`
}

// HostMultipathPath is one path to a LUN.
type HostMultipathPath struct {
	// Name is the runtime name, e.g. vmhba2:C0:T1:L4, and Adapter the host
	// bus adapter it runs through, e.g. vmhba2.
	Name    string `json:"name"`
	Adapter string `json:"adapter,omitempty"`
	// State is active, standby, disabled, dead or unknown.
	State string `json:"state,omitempty"`
}

// multipathLUN reads the LUN identity and paths for one multipath entry.
func multipathLUN(lun types.ScsiLun, paths []types.HostMultipathInfoPath, adapters map[string]string) *HostMultipathLUN {
	out := &HostMultipathLUN{
		CanonicalName:    lun.CanonicalName,
		OperationalState: strings.Join(lun.OperationalState, ", "),
		VStorageSupport:  lun.VStorageSupport,
		Vendor:           lun.Vendor,
		Model:            lun.Model,
		Revision:         lun.Revision,
		Serial:           lun.SerialNumber,
		UUID:             lun.Uuid,
	}
	if lun.QueueDepth != 0 {
		depth := lun.QueueDepth
		out.QueueDepth = &depth
	}
	if lun.ScsiLevel != 0 {
		level := lun.ScsiLevel
		out.SCSILevel = &level
	}
	for _, p := range paths {
		state := p.State
		if state == "" {
			state = p.PathState
		}
		adapter := adapters[p.Adapter]
		if adapter == "" {
			adapter, _, _ = strings.Cut(p.Name, ":")
		}
		out.Paths = append(out.Paths, HostMultipathPath{Name: p.Name, Adapter: adapter, State: state})
	}
	return out
}

// hbaDevices maps each host bus adapter's key to its device name, e.g.
// key-vim.host.BlockHba-vmhba0 to vmhba0.
func hbaDevices(storage *types.HostStorageDeviceInfo) map[string]string {
	out := make(map[string]string, len(storage.HostBusAdapter))
	for _, raw := range storage.HostBusAdapter {
		if raw == nil {
			continue
		}
		if hba := raw.GetHostHostBusAdapter(); hba != nil {
			out[hba.Key] = hba.Device
		}
	}
	return out
}
