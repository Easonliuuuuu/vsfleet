package vsphere

import (
	"fmt"
	"strconv"
	"strings"
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
	KindDatastore Kind = "datastore"
	KindNetwork   Kind = "network"
)

// AllKinds lists every kind the inventory API can enumerate.
var AllKinds = []Kind{KindVM, KindTemplate, KindHost, KindCluster, KindDatastore, KindNetwork}

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
	case "datastore", "datastores", "ds":
		return KindDatastore, nil
	case "network", "networks", "portgroup", "portgroups":
		return KindNetwork, nil
	default:
		return "", fmt.Errorf("unknown resource kind %q (supported: vm, template, host, cluster, datastore, network)", s)
	}
}

// Location says where in the world an object lives. Every domain object embeds
// it, because with several vCenters in play "which one" is part of identity.
type Location struct {
	// Context is the vctui context name, i.e. which vCenter.
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
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	PowerState string   `json:"power_state"`
	IsTemplate bool     `json:"is_template"`
	CPU        int32    `json:"cpu"`
	MemoryMB   int64    `json:"memory_mb"`
	GuestOS    string   `json:"guest_os"`
	GuestState string   `json:"guest_state"`
	ToolsState string   `json:"tools_state"`
	IPAddress  string   `json:"ip_address"`
	Host       string   `json:"host"`
	Cluster    string   `json:"cluster"`
	Folder     string   `json:"folder"`
	Datastores []string `json:"datastores"`
	StorageGB  float64  `json:"storage_gb"`
	Annotation string   `json:"annotation"`
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

// Counts renders a one-line summary, used by status output and by the
// interface's message line.
func (i *Inventory) Counts() string {
	return strings.Join([]string{
		plural(len(i.VMs), "VM"),
		plural(len(i.Templates), "template"),
		plural(len(i.Hosts), "host"),
		plural(len(i.Clusters), "cluster"),
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
