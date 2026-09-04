package vsphere

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Kind names a class of inventory object. Search results and the interface's
// resource tabs are both keyed on it.
type Kind string

// Inventory kinds.
const (
	KindVM        Kind = "vm"
	KindTemplate  Kind = "template"
	KindHost      Kind = "host"
	KindCluster   Kind = "cluster"
	KindVApp      Kind = "vapp"
	KindDatastore Kind = "datastore"
	KindNetwork   Kind = "network"
)

// AllKinds lists every kind the inventory API can enumerate.
var AllKinds = []Kind{KindVM, KindTemplate, KindHost, KindCluster, KindDatastore, KindNetwork, KindVApp}

// ParseKind maps a user-supplied string onto a Kind, tolerating plurals.
func ParseKind(s string) (Kind, error) {
	switch s {
	case "vm", "vms", "virtualmachine", "virtualmachines":
		return KindVM, nil
	case "template", "templates":
		return KindTemplate, nil
	case "host", "hosts", "esxi":
		return KindHost, nil
	case "cluster", "clusters":
		return KindCluster, nil
	case "vapp", "vapps", "virtualapp", "virtualapps":
		return KindVApp, nil
	case "datastore", "datastores", "ds":
		return KindDatastore, nil
	case "network", "networks", "portgroup", "portgroups":
		return KindNetwork, nil
	default:
		return "", fmt.Errorf("unknown resource kind %q (supported: vm, template, host, cluster, vapp, datastore, network)", s)
	}
}

// Location says where in the world an object lives. Every domain object embeds
// it, because with several vCenters in play "which one" is part of identity.
type Location struct {
	// Context is the vsfleet context name, i.e. which vCenter.
	Context string `json:"context"`
	// Datacenter is the vSphere datacenter the object belongs to.
	Datacenter string `json:"datacenter"`
	// Path is the full inventory path, e.g. /Taipei/vm/Templates/ubuntu.
	Path string `json:"path"`
}

// VM is a virtual machine or, when IsTemplate is set, a template. vSphere
// models both with the same managed object, and so does this package: the
// template views filter on IsTemplate rather than using a parallel type.
type VM struct {
	Location
	ID           string       `json:"id"`
	InstanceUUID string       `json:"instance_uuid,omitempty"`
	BIOSUUID     string       `json:"bios_uuid,omitempty"`
	Name         string       `json:"name"`
	PowerState   string       `json:"power_state"`
	IsTemplate   bool         `json:"is_template"`
	CPU          int32        `json:"cpu"`
	MemoryMB     int64        `json:"memory_mb"`
	GuestOS      string       `json:"guest_os"`
	GuestState   string       `json:"guest_state"`
	ToolsState   string       `json:"tools_state"`
	IPAddress    string       `json:"ip_address"`
	Host         string       `json:"host"`
	Cluster      string       `json:"cluster"`
	Folder       string       `json:"folder"`
	Datastores   []string     `json:"datastores"`
	StorageGB    float64      `json:"storage_gb"`
	Annotation   string       `json:"annotation"`
	Disks        []VMDisk     `json:"disks,omitempty"`
	NICs         []VMNIC      `json:"nics,omitempty"`
	Snapshots    []VMSnapshot `json:"snapshots,omitempty"`
}

// VMDisk is one virtual disk from a VM's hardware configuration. Optional
// values remain nil when vSphere does not expose them for a backing type.
type VMDisk struct {
	Key                  int32  `json:"key"`
	Label                string `json:"label"`
	CapacityBytes        int64  `json:"capacity_bytes"`
	UUID                 string `json:"uuid,omitempty"`
	BackingType          string `json:"backing_type,omitempty"`
	BackingPath          string `json:"backing_path,omitempty"`
	Raw                  bool   `json:"raw"`
	DiskMode             string `json:"disk_mode,omitempty"`
	Sharing              string `json:"sharing,omitempty"`
	ThinProvisioned      *bool  `json:"thin_provisioned,omitempty"`
	EagerlyScrub         *bool  `json:"eagerly_scrub,omitempty"`
	Split                *bool  `json:"split,omitempty"`
	WriteThrough         *bool  `json:"write_through,omitempty"`
	SharesLevel          string `json:"shares_level,omitempty"`
	Shares               *int32 `json:"shares,omitempty"`
	Reservation          *int32 `json:"reservation,omitempty"`
	Limit                *int64 `json:"limit,omitempty"`
	Controller           string `json:"controller,omitempty"`
	ControllerLabel      string `json:"controller_label,omitempty"`
	UnitNumber           *int32 `json:"unit_number,omitempty"`
	SharedBus            string `json:"shared_bus,omitempty"`
	RawLUNID             string `json:"raw_lun_id,omitempty"`
	RawCompatibilityMode string `json:"raw_compatibility_mode,omitempty"`
}

// VMNIC is one virtual ethernet adapter and its guest-reported network data.
// NetworkID is retained for joins when a display name is unavailable.
type VMNIC struct {
	Key             int32    `json:"key"`
	Label           string   `json:"label"`
	Adapter         string   `json:"adapter,omitempty"`
	Network         string   `json:"network,omitempty"`
	NetworkID       string   `json:"network_id,omitempty"`
	SwitchID        string   `json:"switch_id,omitempty"`
	MACAddress      string   `json:"mac_address,omitempty"`
	MACAddressType  string   `json:"mac_address_type,omitempty"`
	Connected       *bool    `json:"connected,omitempty"`
	StartsConnected *bool    `json:"starts_connected,omitempty"`
	DirectPathIO    *bool    `json:"direct_path_io,omitempty"`
	IPv4            []string `json:"ipv4,omitempty"`
	IPv6            []string `json:"ipv6,omitempty"`
}

// VMSnapshot is one read-only entry in a VM snapshot tree.
type VMSnapshot struct {
	ID          string    `json:"id"`
	NumericID   int32     `json:"numeric_id"`
	ParentID    string    `json:"parent_id,omitempty"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreateTime  time.Time `json:"create_time"`
	PowerState  string    `json:"power_state"`
	Quiesced    bool      `json:"quiesced"`
	Current     bool      `json:"current"`
}

// Host is an ESXi host.
type Host struct {
	Location
	ID              string `json:"id"`
	Name            string `json:"name"`
	Cluster         string `json:"cluster"`
	PowerState      string `json:"power_state"`
	ConnectionState string `json:"connection_state"`
	InMaintenance   bool   `json:"in_maintenance"`
	Vendor          string `json:"vendor"`
	Model           string `json:"model"`
	Version         string `json:"version"`
	Build           string `json:"build"`
	CPUCores        int32  `json:"cpu_cores"`
	CPUThreads      int32  `json:"cpu_threads"`
	CPUMHz          int32  `json:"cpu_mhz"`
	MemoryMB        int64  `json:"memory_mb"`
	CPUUsageMHz     int64  `json:"cpu_usage_mhz"`
	MemoryUsageMB   int64  `json:"memory_usage_mb"`
	VMCount         int    `json:"vm_count"`
}

// Cluster is a compute cluster. Standalone hosts appear as a ComputeResource
// in vSphere and are reported here with Standalone set.
type Cluster struct {
	Location
	ID            string `json:"id"`
	Name          string `json:"name"`
	Standalone    bool   `json:"standalone"`
	Hosts         int    `json:"hosts"`
	EffectiveHost int    `json:"effective_hosts"`
	CPUCores      int32  `json:"cpu_cores"`
	TotalCPUMHz   int64  `json:"total_cpu_mhz"`
	TotalMemoryMB int64  `json:"total_memory_mb"`
	DRSEnabled    bool   `json:"drs_enabled"`
	HAEnabled     bool   `json:"ha_enabled"`
}

// VApp is a logical vSphere application container. Membership fields contain
// only direct children; nested vApps and resource pools are represented
// separately so a detail view never mistakes descendants for direct members.
type VApp struct {
	Location
	ID                     string   `json:"id"`
	Name                   string   `json:"name"`
	Status                 string   `json:"status"`
	ParentContainer        string   `json:"parent_container"`
	ParentVApp             string   `json:"parent_vapp,omitempty"`
	DirectVMCount          int      `json:"direct_vm_count"`
	DirectVMs              []string `json:"direct_vms"`
	DirectVMRefs           []string `json:"direct_vm_refs,omitempty"`
	ChildVAppCount         int      `json:"child_vapp_count"`
	ChildVApps             []string `json:"child_vapps"`
	ChildResourcePoolCount int      `json:"child_resource_pool_count"`
	ChildResourcePools     []string `json:"child_resource_pools"`
	Cluster                string   `json:"cluster"`
	ComputeResource        string   `json:"compute_resource"`
}

// Datastore is a backing store for VM files.
type Datastore struct {
	Location
	ID            string `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	Accessible    bool   `json:"accessible"`
	CapacityBytes int64  `json:"capacity_bytes"`
	FreeBytes     int64  `json:"free_bytes"`
	Maintenance   string `json:"maintenance"`
}

// UsedBytes is capacity minus free space.
func (d Datastore) UsedBytes() int64 {
	if d.CapacityBytes <= 0 {
		return 0
	}
	return d.CapacityBytes - d.FreeBytes
}

// UsedPercent is the fraction of the datastore in use, 0 when unknown.
func (d Datastore) UsedPercent() float64 {
	if d.CapacityBytes <= 0 {
		return 0
	}
	return float64(d.UsedBytes()) / float64(d.CapacityBytes) * 100
}

// Network is a port group or network the VMs attach to.
type Network struct {
	Location
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Accessible bool   `json:"accessible"`
}

// Inventory is everything enumerated from one vCenter at one moment.
//
// A partial result is a valid result: ListInventory keeps going after one
// kind fails to list — a limited-permission account that can see VMs but not
// Datastores is a normal shape, not a reason to discard everything it could
// read. Errors records what did not come back; every kind missing from it
// enumerated cleanly, even if empty.
type Inventory struct {
	Context    string           `json:"context"`
	VMs        []VM             `json:"vms"`
	Templates  []VM             `json:"templates"`
	Hosts      []Host           `json:"hosts"`
	Clusters   []Cluster        `json:"clusters"`
	VApps      []VApp           `json:"vapps"`
	Datastores []Datastore      `json:"datastores"`
	Networks   []Network        `json:"networks"`
	Errors     []InventoryError `json:"errors,omitempty"`
}

// InventoryError is one resource kind ListInventory could not enumerate.
type InventoryError struct {
	Kind    Kind   `json:"kind"`
	Message string `json:"message"`
}

// ErrorFor returns the reason kind failed to list, or false if it enumerated
// cleanly (which includes kinds this Inventory never attempted).
func (i *Inventory) ErrorFor(kind Kind) (string, bool) {
	for _, e := range i.Errors {
		if e.Kind == kind {
			return e.Message, true
		}
	}
	return "", false
}

// Slice extracts one fetch group's share of i as its own Inventory — the
// inverse of ApplyGroup, and the counterpart a Backend whose data is already
// fully assembled (a fixed test fixture, a demo estate) uses to answer
// InventoryHandle.FetchGroup without a real per-group retrieval behind it.
func (i *Inventory) Slice(group FetchGroup) *Inventory {
	part := &Inventory{Context: i.Context}
	switch group {
	case GroupVMs:
		part.VMs, part.Templates = i.VMs, i.Templates
	case GroupHosts:
		part.Hosts = i.Hosts
	case GroupClusters:
		part.Clusters = i.Clusters
	case GroupVApps:
		part.VApps = i.VApps
	case GroupDatastores:
		part.Datastores = i.Datastores
	case GroupNetworks:
		part.Networks = i.Networks
	}
	for _, e := range i.Errors {
		if GroupFor(e.Kind) == group {
			part.Errors = append(part.Errors, e)
		}
	}
	return part
}

// ApplyGroup folds one FetchGroup's result into i: on success it replaces
// i's fields for that group's kinds with part's; on failure it leaves them
// exactly as they were. i.Errors is updated for the group's kinds either
// way, replacing whatever i previously recorded for them.
//
// This is what lets a refresh that fails for one kind keep showing the last
// data that kind did have while every other kind still updates normally —
// stale-while-revalidate applied per kind, the same promise ListInventory
// already made per context. It is also what a first-ever load uses to
// assemble the whole Inventory: called once per group against a blank
// Inventory, "leave it as it was" and "replace it" agree, since a blank
// field either way is blank.
func (i *Inventory) ApplyGroup(group FetchGroup, part *Inventory) {
	failed := len(part.Errors) > 0
	kept := i.Errors[:0]
	for _, e := range i.Errors {
		if GroupFor(e.Kind) != group {
			kept = append(kept, e)
		}
	}
	i.Errors = append(kept, part.Errors...)
	if failed {
		return
	}
	switch group {
	case GroupVMs:
		i.VMs = part.VMs
		i.Templates = part.Templates
	case GroupHosts:
		i.Hosts = part.Hosts
	case GroupClusters:
		i.Clusters = part.Clusters
	case GroupVApps:
		i.VApps = part.VApps
	case GroupDatastores:
		i.Datastores = part.Datastores
	case GroupNetworks:
		i.Networks = part.Networks
	}
}

// Counts renders a one-line summary, used by status output and by the
// interface's message line.
func (i *Inventory) Counts() string {
	return strings.Join([]string{
		plural(len(i.VMs), "VM"),
		plural(len(i.Templates), "template"),
		plural(len(i.Hosts), "host"),
		plural(len(i.Clusters), "cluster"),
		plural(len(i.VApps), "vApp"),
		plural(len(i.Datastores), "datastore"),
		plural(len(i.Networks), "network"),
	}, ", ")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
