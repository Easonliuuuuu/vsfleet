package report

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// maxMultipathPaths is how many paths a vMultiPath row lists, as in RVTools.
const maxMultipathPaths = 8

var (
	// datastoreHeaders are RVTools 4.8's vDatastore columns in its order,
	// with vsfleet's Datacenter after Name and Maintenance mode after MHA.
	datastoreHeaders = []string{"Name", "Datacenter", "Config status", "Address", "Accessible", "Type", "# VMs total", "# VMs", "Capacity MiB", "Provisioned MiB", "In Use MiB", "Free MiB", "Free %",
		"SIOC enabled", "SIOC Threshold", "# Hosts", "Hosts", "Cluster name", "Cluster capacity MiB", "Cluster free space MiB", "Block size", "Max Blocks", "# Extents", "Major Version", "Version",
		"VMFS Upgradeable", "MHA", "Maintenance mode", "URL", "Object ID", "VI SDK Server", "VI SDK UUID", "vsfleet Context"}
	// multipathHeaders are RVTools 4.8's vMultiPath columns in its order,
	// with vsfleet's path counts before Object ID, which names the datastore.
	multipathHeaders = append(append([]string{"Host", "Cluster", "Datacenter", "Datastore", "Disk", "Display name", "Policy", "Oper. State"}, multipathPathHeaders()...),
		"vStorage", "Queue depth", "Vendor", "Model", "Revision", "Level", "Serial #", "UUID",
		"Device path", "Local disk", "Path count", "Active paths", "Standby paths", "Dead paths", "Disabled paths", "Working paths",
		"Object ID", "VI SDK Server", "VI SDK UUID", "vsfleet Context")
)

func multipathPathHeaders() []string {
	out := make([]string, 0, 2*maxMultipathPaths)
	for i := 1; i <= maxMultipathPaths; i++ {
		out = append(out, fmt.Sprintf("Path %d", i), fmt.Sprintf("Path %d state", i))
	}
	return out
}

func datastoreRows(data assessment.ExportData) [][]any {
	powered := vmPowerStates(data)
	rows := make([][]any, 0)
	for _, r := range data.Resources {
		if r.Kind != "datastore" {
			continue
		}
		var datastore vsphere.Datastore
		if err := json.Unmarshal(r.Payload, &datastore); err != nil {
			continue
		}
		capacity, free := float64(datastore.CapacityBytes)/miB, float64(datastore.FreeBytes)/miB
		var freePercent any
		if datastore.CapacityBytes > 0 {
			freePercent = float64(datastore.FreeBytes) / float64(datastore.CapacityBytes) * 100
		}
		d := datastoreDetailsOf(datastore)
		row := []any{cmp.Or(datastore.Name, r.Name), datastore.Datacenter, optionalString(d.ConfigStatus), optionalString(d.Address), datastore.Accessible, datastore.Type}
		row = append(row, datastoreVMCounts(datastore.Details, powered, r.Context)...)
		row = append(row, capacity, datastoreProvisionedMiB(datastore), float64(datastore.UsedBytes())/miB, free, freePercent, optionalBool(d.SIOCEnabled), optionalInt32(d.SIOCThreshold))
		row = append(row, datastoreHostCells(datastore.Details)...)
		row = append(row, datastoreClusterCells(d.StorageCluster)...)
		row = append(row, datastoreVMFSCells(datastore.Details)...)
		row = append(row, optionalBool(d.MultipleHostAccess), datastore.Maintenance, optionalString(datastore.Backing.URL), cmp.Or(datastore.ID, r.ID), sdkServer(data, r.Context), r.VCenterID, r.Context)
		rows = append(rows, row)
	}
	return rows
}

// datastoreDetailsOf returns the datastore's details, empty for a capture
// predating inventory schema 35, whose cells stay blank.
func datastoreDetailsOf(d vsphere.Datastore) vsphere.DatastoreDetails {
	if d.Details == nil {
		return vsphere.DatastoreDetails{}
	}
	return *d.Details
}

// vmPowerStates is each VM's power state, by context and VM ID, with
// "template" for a template.
func vmPowerStates(data assessment.ExportData) map[[2]string]string {
	out := make(map[[2]string]string, len(data.VMs))
	for _, item := range data.VMs {
		state := item.Observation.VM.PowerState
		if item.Observation.VM.IsTemplate {
			state = "template"
		}
		out[[2]string{item.Observation.Context, item.Observation.VM.ID}] = state
	}
	return out
}

// datastoreVMCounts writes # VMs total, the VMs with files on the datastore,
// and # VMs, the powered-on ones among them. Templates are left out, as
// RVTools leaves them out; a VM the capture did not record counts, as off.
func datastoreVMCounts(d *vsphere.DatastoreDetails, powered map[[2]string]string, context string) []any {
	if d == nil || d.VMIDs == nil {
		return []any{nil, nil}
	}
	total, on := 0, 0
	for _, id := range d.VMIDs {
		switch powered[[2]string{context, id}] {
		case "template":
			continue
		case "poweredOn":
			on++
		}
		total++
	}
	return []any{total, on}
}

func datastoreHostCells(d *vsphere.DatastoreDetails) []any {
	if d == nil {
		return []any{nil, nil}
	}
	return []any{len(d.Hosts), optionalString(strings.Join(d.Hosts, ", "))}
}

func datastoreClusterCells(c *vsphere.DatastoreCluster) []any {
	if c == nil {
		return []any{nil, nil, nil}
	}
	return []any{optionalString(c.Name), bytesMiB(c.CapacityBytes), bytesMiB(c.FreeBytes)}
}

// datastoreVMFSCells writes Block size through VMFS Upgradeable. RVTools
// writes zero sizes and counts for a datastore that is not VMFS, and leaves
// its version and upgradeability empty.
func datastoreVMFSCells(d *vsphere.DatastoreDetails) []any {
	if d == nil {
		return make([]any, 6)
	}
	v := d.VMFS
	if v == nil {
		return []any{0, 0, 0, 0, nil, nil}
	}
	return []any{v.BlockSizeMB, v.MaxBlocks, v.Extents, v.MajorVersion, optionalString(v.Version), v.Upgradable}
}

// datastoreLUN is a datastore an extent of which is on a LUN.
type datastoreLUN struct{ name, id string }

// multipathRows writes one row per host and LUN that backs a datastore, as
// RVTools does: a LUN no datastore is on, such as an ESXi boot disk, is left
// out here and stays in the host's capture and in host-path-redundancy.
func multipathRows(data assessment.ExportData) [][]any {
	byLUN := make(map[[2]string][]datastoreLUN)
	for _, r := range data.Resources {
		if r.Kind != "datastore" {
			continue
		}
		var d vsphere.Datastore
		if json.Unmarshal(r.Payload, &d) != nil {
			continue
		}
		for _, extent := range d.Backing.Extents {
			key := [2]string{r.Context, extent}
			byLUN[key] = append(byLUN[key], datastoreLUN{cmp.Or(d.Name, r.Name), cmp.Or(d.ID, r.ID)})
		}
	}
	rows := make([][]any, 0)
	for _, item := range hostConfigResources(data) {
		host, resource := item.host, item.resource
		for _, mp := range host.Multipaths {
			disk := mp.CanonicalName()
			for _, ds := range byLUN[[2]string{resource.Context, disk}] {
				row := []any{cmp.Or(host.Name, resource.Name), host.Cluster, host.Datacenter, ds.name, optionalString(disk), optionalString(mp.LUN), optionalString(mp.Policy)}
				row = append(row, multipathDeviceCells(mp.Device)...)
				row = append(row, optionalString(mp.DevicePath), optionalBool(mp.LocalDisk), mp.PathCount, mp.Active, mp.Standby, mp.Dead, mp.Disabled, mp.WorkingPaths)
				rows = append(rows, append(row, ds.id, sdkServer(data, resource.Context), resource.VCenterID, resource.Context))
			}
		}
	}
	return rows
}

// multipathDeviceCells writes Oper. State, the eight path pairs and vStorage
// through UUID; empty for a capture predating inventory schema 35.
func multipathDeviceCells(d *vsphere.HostMultipathLUN) []any {
	cells := make([]any, 1+2*maxMultipathPaths+8)
	if d == nil {
		return cells
	}
	cells[0] = optionalString(d.OperationalState)
	for i, p := range d.Paths {
		if i == maxMultipathPaths {
			break
		}
		cells[1+2*i], cells[2+2*i] = optionalString(cmp.Or(p.Adapter, p.Name)), optionalString(p.State)
	}
	tail := []any{optionalString(d.VStorageSupport), optionalInt32(d.QueueDepth), optionalString(d.Vendor), optionalString(d.Model), optionalString(d.Revision),
		optionalInt32(d.SCSILevel), optionalString(d.Serial), optionalString(d.UUID)}
	copy(cells[1+2*maxMultipathPaths:], tail)
	return cells
}
