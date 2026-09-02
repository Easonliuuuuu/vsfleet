package vsphere

import "fmt"

// Kind names a class of inventory object. Search results and the future UI
// tabs are both keyed on it.
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
	Context string
	// Datacenter is the vSphere datacenter the object belongs to.
	Datacenter string
	// Path is the full inventory path, e.g. /Taipei/vm/Templates/ubuntu.
	Path string
}

// VM is a virtual machine or, when IsTemplate is set, a template. vSphere
// models both with the same managed object, and so does this package: the
// template views filter on IsTemplate rather than using a parallel type.
type VM struct {
	Location
	ID         string
	Name       string
	PowerState string
	IsTemplate bool
	CPU        int32
	MemoryMB   int64
	GuestOS    string
	GuestState string
	ToolsState string
	IPAddress  string
	Host       string
	Cluster    string
	Folder     string
	Datastores []string
	StorageGB  float64
	Annotation string
}

// Host is an ESXi host.
type Host struct {
	Location
	ID              string
	Name            string
	Cluster         string
	PowerState      string
	ConnectionState string
	InMaintenance   bool
	Vendor          string
	Model           string
	Version         string
	Build           string
	CPUCores        int32
	CPUThreads      int32
	CPUMHz          int32
	MemoryMB        int64
	CPUUsageMHz     int64
	MemoryUsageMB   int64
	VMCount         int
}

// Cluster is a compute cluster. Standalone hosts appear as a ComputeResource
// in vSphere and are reported here with Standalone set.
type Cluster struct {
	Location
	ID            string
	Name          string
	Standalone    bool
	Hosts         int
	EffectiveHost int
	CPUCores      int32
	TotalCPUMHz   int64
	TotalMemoryMB int64
	DRSEnabled    bool
	HAEnabled     bool
}

// Datastore is a backing store for VM files.
type Datastore struct {
	Location
	ID            string
	Name          string
	Type          string
	Accessible    bool
	CapacityBytes int64
	FreeBytes     int64
	Maintenance   string
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
	ID         string
	Name       string
	Type       string
	Accessible bool
}

// Inventory is everything enumerated from one vCenter at one moment.
type Inventory struct {
	Context    string
	VMs        []VM
	Templates  []VM
	Hosts      []Host
	Clusters   []Cluster
	Datastores []Datastore
	Networks   []Network
}

// Counts renders a one-line summary, used by status output.
func (i *Inventory) Counts() string {
	return fmt.Sprintf("%d VMs, %d templates, %d hosts, %d clusters, %d datastores, %d networks",
		len(i.VMs), len(i.Templates), len(i.Hosts), len(i.Clusters), len(i.Datastores), len(i.Networks))
}
