// Package demo provides deterministic, synthetic vCenter data for recording
// and presenting the terminal interface. It never opens a network connection
// or reads configuration and credentials from the operator's machine.
package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/contextops"
	"github.com/easonliuuuuu/vsfleet/internal/session"
	"github.com/easonliuuuuu/vsfleet/internal/tui"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// Backend is a read-only TUI backend populated with sample inventory. It is
// intentionally separate from the production command so demo data can never
// be mistaken for a live vCenter.
type Backend struct {
	contexts    []*config.Context
	inventories map[string]*vsphere.Inventory
	failures    map[string]error
	diagnoses   map[string]*vsphere.Diagnosis
}

// NewBackend returns a stable three-vCenter estate: two healthy contexts with
// different routes and one unreachable disaster-recovery site. That shape
// demonstrates the central promise of vsfleet: healthy results remain useful
// when another vCenter is offline.
func NewBackend() *Backend {
	contexts := []*config.Context{
		newContext("prod-vc", "https://vcsa.prod.example", config.TransportConfig{Type: config.TransportDirect}),
		newContext("edge-vc", "https://vcsa.edge.example", config.TransportConfig{
			Type: config.TransportSOCKS5, Address: "127.0.0.1:1080", RemoteDNS: true,
		}),
		newContext("dr-site", "https://vcsa.dr.example", config.TransportConfig{
			Type: config.TransportHTTPProxy, Address: "10.24.0.8:3128",
		}),
	}

	prodInventory := sampleInventory("prod-vc", "Taipei", "10.20.0")
	seedMigrationDefaults(prodInventory)
	addDemoHealthEvidence(prodInventory)
	edgeInventory := sampleInventory("edge-vc", "Hsinchu", "10.42.0")
	seedMigrationDefaults(edgeInventory)
	addDemoSharedDatastoreEvidence(edgeInventory)
	return &Backend{
		contexts: contexts,
		inventories: map[string]*vsphere.Inventory{
			"prod-vc": prodInventory,
			"edge-vc": edgeInventory,
		},
		failures: map[string]error{
			"dr-site": errors.New("proxy 10.24.0.8:3128: connection refused"),
		},
		diagnoses: map[string]*vsphere.Diagnosis{
			"prod-vc": healthyDiagnosis(contexts[0], 84*time.Millisecond),
			"edge-vc": healthyDiagnosis(contexts[1], 127*time.Millisecond),
			"dr-site": failedDiagnosis(contexts[2]),
		},
	}
}

// addDemoHealthEvidence keeps the presentation estate useful for the health
// workstream too: one VM is orphaned and one VMDK is visible to the optional
// datastore browser but is not referenced by any VM or template.
func addDemoHealthEvidence(inv *vsphere.Inventory) {
	if inv == nil || len(inv.VMs) < 3 || len(inv.Templates) == 0 || len(inv.Datastores) == 0 {
		return
	}
	inv.VMs[2].ConnectionState = "orphaned"
	inv.VMs[0].Disks = []vsphere.VMDisk{{Key: 101, Label: "Hard disk 1", CapacityBytes: 80 << 30, BackingPath: "[nvme-01] api-01/api-01.vmdk"}}
	inv.VMs[2].Disks = []vsphere.VMDisk{{Key: 102, Label: "Hard disk 1", CapacityBytes: 120 << 30, BackingPath: "[nvme-01] build-runner-03/build-runner-03.vmdk"}}
	inv.Templates[0].Disks = []vsphere.VMDisk{{Key: 103, Label: "Hard disk 1", CapacityBytes: 16 << 30, BackingPath: "[nvme-01] templates/ubuntu-24.04-golden.vmdk"}}
	inv.Datastores[0].BrowseStatus = "success"
	inv.Datastores[0].Backing = vsphere.DatastoreBacking{VMFSUUID: "demo-vmfs-nvme-01", Extents: []string{"naa.demo.6000"}}
	inv.Datastores[0].Files = []vsphere.DatastoreFile{
		{Path: "[nvme-01] api-01/api-01.vmdk", SizeBytes: 80 << 30},
		{Path: "[nvme-01] api-01/api-01-000001.vmdk", SizeBytes: 4 << 30},
		{Path: "[nvme-01] build-runner-03/build-runner-03.vmdk", SizeBytes: 120 << 30},
		{Path: "[nvme-01] lost+found/orphan.vmdk", SizeBytes: 20 << 30},
		{Path: "[nvme-01] finance01/finance01.vmdk", SizeBytes: 420 << 30, Modified: time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)},
		{Path: "[nvme-01] templates/ubuntu-24.04-golden.vmdk", SizeBytes: 16 << 30},
	}
}

func addDemoSharedDatastoreEvidence(inv *vsphere.Inventory) {
	if inv == nil || len(inv.VMs) == 0 || len(inv.Datastores) == 0 {
		return
	}
	inv.Datastores[0].Name = "san-prod-01"
	inv.Datastores[0].Path = strings.Replace(inv.Datastores[0].Path, "nvme-01", "san-prod-01", 1)
	inv.Datastores[0].Backing = vsphere.DatastoreBacking{VMFSUUID: "demo-vmfs-nvme-01", Extents: []string{"naa.demo.6000"}}
	inv.Datastores[0].BrowseStatus = "success"
	inv.Datastores[0].Files = []vsphere.DatastoreFile{{Path: "[san-prod-01] finance01/finance01.vmdk", SizeBytes: 420 << 30, Modified: time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)}}
	inv.VMs[0].Datastores = []string{"san-prod-01"}
	inv.VMs[0].Disks = []vsphere.VMDisk{{Key: 201, Label: "Hard disk 1", CapacityBytes: 420 << 30, BackingPath: "[san-prod-01] finance01/finance01.vmdk"}}
}

func newContext(name, endpoint string, route config.TransportConfig) *config.Context {
	cc := &config.Context{
		Name: name, Endpoint: endpoint, Username: "operator@vsphere.local",
		Transport: route, TLS: config.TLSConfig{Mode: config.TLSSystem},
	}
	cc.Normalize()
	return cc
}

// Contexts implements tui.Backend.
func (b *Backend) Contexts() []*config.Context { return b.contexts }

// BeginInventory implements tui.Backend. The demo estate has each context's
// whole Inventory assembled up front, so the "handle" it hands back simply
// slices that fixed Inventory into the group tui.InventoryHandle.FetchGroup
// asks for, rather than actually connecting to anything.
func (b *Backend) BeginInventory(_ context.Context, cc *config.Context) (tui.InventoryHandle, error) {
	if err := b.failures[cc.Name]; err != nil {
		return nil, err
	}
	inv, ok := b.inventories[cc.Name]
	if !ok {
		return nil, fmt.Errorf("demo inventory for %q not found", cc.Name)
	}
	return demoInventoryHandle{inv: inv}, nil
}

// demoInventoryHandle answers each fetch group from the demo estate's
// pre-built Inventory, so recording or presenting the interface never opens
// a real connection.
type demoInventoryHandle struct{ inv *vsphere.Inventory }

// FetchGroup implements tui.InventoryHandle. The whole estate is already in
// memory, so the group is one page and partial sees it once — enough to keep
// the streaming path exercised by the demo rather than only in production.
func (h demoInventoryHandle) FetchGroup(group vsphere.FetchGroup, partial func(*vsphere.Inventory)) *vsphere.Inventory {
	part := h.inv.Slice(group)
	if partial != nil {
		partial(h.inv.Slice(group))
	}
	return part
}

// Status implements tui.Backend.
func (b *Backend) Status(name string) (session.Status, bool) {
	d := b.diagnoses[name]
	if d == nil {
		return session.Status{}, false
	}
	return session.Status{Name: name, Latency: d.Latency}, true
}

// Diagnose implements tui.Backend.
func (b *Backend) Diagnose(_ context.Context, cc *config.Context) *vsphere.Diagnosis {
	return b.diagnoses[cc.Name]
}

// AssessmentService returns a seeded, in-memory history service for the
// presentation demo. It lets the History health pane show the same orphaned
// VM and zombie-VMDK evidence as the inventory without touching disk.
func (b *Backend) AssessmentService() (*assessment.Service, func(), error) {
	store, err := assessment.OpenMemory()
	if err != nil {
		return nil, nil, err
	}
	closeStore := func() { _ = store.Close() }
	cc := b.contexts[0]
	inv := b.inventories[cc.Name]
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	run, err := store.StartRunWithMetadata(context.Background(), "demo", b.contexts, now, assessment.RunMetadata{InventorySchemaVersion: assessment.CurrentInventorySchemaVersion})
	if err != nil {
		closeStore()
		return nil, nil, err
	}
	observations := make([]assessment.Observation, 0, len(inv.VMs)+len(inv.Templates))
	for _, vm := range append(append([]vsphere.VM(nil), inv.VMs...), inv.Templates...) {
		observations = append(observations, assessment.Observation{Context: cc.Name, VCenterID: "demo-prod-vc", VM: vm})
	}
	collections := []assessment.CollectionResult{
		{Kind: "vm", Status: "success", ItemCount: len(observations)},
		{Kind: "host", Status: "success", ItemCount: len(inv.Hosts), Resources: demoResources(cc.Name, "demo-prod-vc", "host", inv.Hosts)},
		{Kind: "cluster", Status: "success", ItemCount: len(inv.Clusters), Resources: demoResources(cc.Name, "demo-prod-vc", "cluster", inv.Clusters)},
		{Kind: "resourcepool", Status: "success", ItemCount: len(inv.ResourcePools), Resources: demoResources(cc.Name, "demo-prod-vc", "resourcepool", inv.ResourcePools)},
		{Kind: "datastore", Status: "success", ItemCount: len(inv.Datastores), Resources: demoResources(cc.Name, "demo-prod-vc", "datastore", inv.Datastores)},
		{Kind: "dvswitch", Status: "success", ItemCount: len(inv.DVSwitches), Resources: demoResources(cc.Name, "demo-prod-vc", "dvswitch", inv.DVSwitches)},
		{Kind: "network", Status: "success", ItemCount: len(inv.Networks), Resources: demoResources(cc.Name, "demo-prod-vc", "network", inv.Networks)},
	}
	if err := store.SaveContext(context.Background(), run.ID, assessment.ContextResult{Name: cc.Name, VCenterID: "demo-prod-vc", Status: "success", VMs: observations, Collections: collections}, now.Add(time.Minute)); err != nil {
		closeStore()
		return nil, nil, err
	}
	edge := b.contexts[1]
	edgeInv := b.inventories[edge.Name]
	edgeObservations := make([]assessment.Observation, 0, len(edgeInv.VMs)+len(edgeInv.Templates))
	for _, vm := range append(append([]vsphere.VM(nil), edgeInv.VMs...), edgeInv.Templates...) {
		edgeObservations = append(edgeObservations, assessment.Observation{Context: edge.Name, VCenterID: "demo-edge-vc", VM: vm})
	}
	edgeCollections := []assessment.CollectionResult{
		{Kind: "vm", Status: "success", ItemCount: len(edgeObservations)},
		{Kind: "host", Status: "success", ItemCount: len(edgeInv.Hosts), Resources: demoResources(edge.Name, "demo-edge-vc", "host", edgeInv.Hosts)},
		{Kind: "cluster", Status: "success", ItemCount: len(edgeInv.Clusters), Resources: demoResources(edge.Name, "demo-edge-vc", "cluster", edgeInv.Clusters)},
		{Kind: "resourcepool", Status: "success", ItemCount: len(edgeInv.ResourcePools), Resources: demoResources(edge.Name, "demo-edge-vc", "resourcepool", edgeInv.ResourcePools)},
		{Kind: "datastore", Status: "success", ItemCount: len(edgeInv.Datastores), Resources: demoResources(edge.Name, "demo-edge-vc", "datastore", edgeInv.Datastores)},
		{Kind: "dvswitch", Status: "success", ItemCount: len(edgeInv.DVSwitches), Resources: demoResources(edge.Name, "demo-edge-vc", "dvswitch", edgeInv.DVSwitches)},
		{Kind: "network", Status: "success", ItemCount: len(edgeInv.Networks), Resources: demoResources(edge.Name, "demo-edge-vc", "network", edgeInv.Networks)},
	}
	if err := store.SaveContext(context.Background(), run.ID, assessment.ContextResult{Name: edge.Name, VCenterID: "demo-edge-vc", Status: "success", VMs: edgeObservations, Collections: edgeCollections}, now.Add(time.Minute)); err != nil {
		closeStore()
		return nil, nil, err
	}
	dr := b.contexts[2]
	if err := store.SaveContext(context.Background(), run.ID, assessment.ContextResult{Name: dr.Name, VCenterID: "demo-dr-site", Status: "failed", Error: "proxy 10.24.0.8:3128: connection refused", Collections: []assessment.CollectionResult{{Kind: "vm", Status: "failed", Error: "proxy connection refused"}, {Kind: "host", Status: "failed"}, {Kind: "cluster", Status: "failed"}, {Kind: "resourcepool", Status: "failed"}, {Kind: "dvswitch", Status: "failed"}, {Kind: "datastore", Status: "failed"}, {Kind: "network", Status: "failed"}}}, now.Add(time.Minute)); err != nil {
		closeStore()
		return nil, nil, err
	}
	if _, err := store.FinishRun(context.Background(), run.ID, now.Add(2*time.Minute)); err != nil {
		closeStore()
		return nil, nil, err
	}
	return &assessment.Service{Store: store}, closeStore, nil
}

func demoResources(contextName, vcenterID, kind string, values any) []assessment.ResourceObservation {
	var resources []assessment.ResourceObservation
	switch typed := values.(type) {
	case []vsphere.Host:
		for _, value := range typed {
			resources = append(resources, makeDemoResource(contextName, vcenterID, kind, value.ID, value.Name, value))
		}
	case []vsphere.Cluster:
		for _, value := range typed {
			resources = append(resources, makeDemoResource(contextName, vcenterID, kind, value.ID, value.Name, value))
		}
	case []vsphere.ResourcePool:
		for _, value := range typed {
			resources = append(resources, makeDemoResource(contextName, vcenterID, kind, value.ID, value.Name, value))
		}
	case []vsphere.Datastore:
		for _, value := range typed {
			resources = append(resources, makeDemoResource(contextName, vcenterID, kind, value.ID, value.Name, value))
		}
	case []vsphere.DVSwitch:
		for _, value := range typed {
			resources = append(resources, makeDemoResource(contextName, vcenterID, kind, value.ID, value.Name, value))
		}
	case []vsphere.Network:
		for _, value := range typed {
			resources = append(resources, makeDemoResource(contextName, vcenterID, kind, value.ID, value.Name, value))
		}
	}
	return resources
}

func makeDemoResource(contextName, vcenterID, kind, id, name string, value any) assessment.ResourceObservation {
	payload, _ := json.Marshal(value)
	return assessment.ResourceObservation{Context: contextName, VCenterID: vcenterID, Kind: kind, ID: id, Name: name, Payload: payload}
}

func int64Value(value int64) *int64 { return &value }
func int32Value(value int32) *int32 { return &value }
func boolValue(value bool) *bool    { return &value }

func seedMigrationDefaults(inv *vsphere.Inventory) {
	if inv == nil {
		return
	}
	for i := range inv.VMs {
		seedMigrationVM(&inv.VMs[i])
	}
	for i := range inv.Templates {
		seedMigrationVM(&inv.Templates[i])
	}
}

func seedMigrationVM(vm *vsphere.VM) {
	if vm == nil {
		return
	}
	zero := int64(0)
	unlimited := int64(-1)
	secureBoot, autoCores, locked := false, false, false
	vm.ConfigurationAvailable = true
	vm.Firmware = "efi"
	vm.SecureBootEnabled = &secureBoot
	vm.CoresPerSocket = 1
	vm.CPUSockets = vm.CPU
	vm.AutoCoresPerSocket = &autoCores
	vm.CPUAllocation = &vsphere.VMResourceAllocation{Reservation: &zero, Limit: &unlimited}
	vm.MemoryAllocation = &vsphere.VMResourceAllocation{Reservation: &zero, Limit: &unlimited}
	vm.MemoryReservationLockedToMax = &locked
}

// The remaining methods satisfy the TUI backend contract. The presentation is
// deliberately read-only so a recording cannot imply that sample contexts can
// be changed or saved.
func (b *Backend) TestContext(context.Context, contextops.Input) (*config.Context, *vsphere.Diagnosis) {
	return nil, nil
}

func (b *Backend) SaveContext(context.Context, contextops.Input, bool) (*contextops.Result, error) {
	return nil, errors.New("the presentation is read-only")
}

func (b *Backend) RemoveContext(context.Context, string, bool) (*config.Context, error) {
	return nil, errors.New("the presentation is read-only")
}

func (b *Backend) DiscoverThumbprint(context.Context, *config.Context) (string, string, string, time.Time, error) {
	return "", "", "", time.Time{}, errors.New("the presentation is read-only")
}

func sampleInventory(name, datacenter, subnet string) *vsphere.Inventory {
	loc := func(kind, object string) vsphere.Location {
		return vsphere.Location{
			Context: name, Datacenter: datacenter,
			Path: "/" + datacenter + "/" + kind + "/" + object,
		}
	}
	vappLoc := func(object string) vsphere.Location {
		return vsphere.Location{
			Context: name, Datacenter: datacenter,
			Path: "/" + datacenter + "/host/compute-a/Resources/" + object,
		}
	}
	return &vsphere.Inventory{
		Context: name,
		VMs: []vsphere.VM{
			{Location: loc("vm", "api-01"), ID: name + "-vm-1", Name: "api-01", PowerState: "poweredOn", CPU: 4, MemoryMB: 16384, GuestOS: "Ubuntu Linux (64-bit)", IPAddress: subnet + ".11", Host: "esxi-01", Cluster: "compute-a", Folder: "/Applications", Datastores: []string{"nvme-01"}, StorageGB: 80, Annotation: "customer API"},
			{Location: loc("vm", "postgres-01"), ID: name + "-vm-2", Name: "postgres-01", PowerState: "poweredOn", CPU: 8, MemoryMB: 32768, GuestOS: "Ubuntu Linux (64-bit)", IPAddress: subnet + ".21", Host: "esxi-02", Cluster: "compute-a", Folder: "/Databases", Datastores: []string{"san-01"}, StorageGB: 512, Annotation: "primary database"},
			{Location: loc("vm", "build-runner-03"), ID: name + "-vm-3", Name: "build-runner-03", PowerState: "poweredOff", CPU: 8, MemoryMB: 24576, GuestOS: "VMware Photon OS (64-bit)", Host: "esxi-03", Cluster: "compute-b", Folder: "/Platform", Datastores: []string{"nvme-01"}, StorageGB: 120},
		},
		Templates: []vsphere.VM{
			{Location: loc("vm", "ubuntu-24.04-golden"), ID: name + "-tpl-1", Name: "ubuntu-24.04-golden", IsTemplate: true, CPU: 2, MemoryMB: 4096, GuestOS: "Ubuntu Linux (64-bit)", StorageGB: 16},
			{Location: loc("vm", "windows-2025-core"), ID: name + "-tpl-2", Name: "windows-2025-core", IsTemplate: true, CPU: 4, MemoryMB: 8192, GuestOS: "Microsoft Windows Server 2025", StorageGB: 64},
		},
		Hosts: []vsphere.Host{
			{Location: loc("host", "esxi-01"), ID: name + "-host-1", Name: "esxi-01", Cluster: "compute-a", PowerState: "poweredOn", ConnectionState: "connected", Vendor: "Dell Inc.", Model: "PowerEdge R750", Version: "8.0.3", Build: "24022515", CPUCores: 32, CPUThreads: 64, CPUMHz: 2400, TotalCPUMHz: 76800, MemoryMB: 524288, CPUUsageMHz: 18400, MemoryUsageMB: 244000, VMCount: 31,
				HBAs:       []vsphere.HostHBA{{Key: name + "-hba-1", Device: "vmhba0", Bus: 3, Status: "online", Model: "QLogic 2692", Driver: "qlnativefc", PCI: "0000:5e:00.0", StorageProtocol: "fc", Type: "HostFibreChannelHba", WWNN: int64Value(0x50014380242a1234), WWPN: int64Value(0x50014380242a1235)}},
				NICs:       []vsphere.HostNIC{{Key: name + "-pnic-1", Device: "vmnic0", PCI: "0000:18:00.0", Driver: "i40en", MAC: "00:50:56:aa:01:01", LinkSpeedMB: int32Value(10000), Duplex: boolValue(true), WakeOnLAN: false, Switch: "vSwitch0"}},
				VSwitches:  []vsphere.HostVSwitch{{Key: name + "-switch-1", Name: "vSwitch0", NumPorts: 128, FreePorts: 120, MTU: 1500, Uplinks: []string{"vmnic0"}, Promiscuous: boolValue(false), MACChanges: boolValue(true), ForgedTransmits: boolValue(true), TrafficShaping: boolValue(false)}},
				PortGroups: []vsphere.HostPortGroup{{Key: name + "-port-1", Name: "Management Network", Switch: "vSwitch0", VLAN: 120, Promiscuous: boolValue(false), MACChanges: boolValue(true), ForgedTransmits: boolValue(true)}},
				VMKs:       []vsphere.HostVMKernel{{Key: name + "-vmk-1", Device: "vmk0", PortGroup: "Management Network", MAC: "00:50:56:aa:01:02", MTU: 1500, TSO: boolValue(true), Netstack: "defaultTcpipStack", DHCP: boolValue(false), IP: subnet + ".10", SubnetMask: "255.255.255.0", ServiceConsole: false}},
				Multipaths: []vsphere.HostMultipath{{Key: name + "-lun-1", LUN: "naa.60060160.example.0001", DevicePath: "/vmfs/devices/disks/naa.60060160.example.0001", Policy: "VMW_PSP_RR", PathCount: 2, Active: 1, Standby: 1, WorkingPaths: 1}},
			},
			{Location: loc("host", "esxi-02"), ID: name + "-host-2", Name: "esxi-02", Cluster: "compute-a", PowerState: "poweredOn", ConnectionState: "connected", Vendor: "Dell Inc.", Model: "PowerEdge R750", Version: "8.0.3", Build: "24022515", CPUCores: 32, CPUThreads: 64, CPUMHz: 2400, TotalCPUMHz: 76800, MemoryMB: 524288, CPUUsageMHz: 22100, MemoryUsageMB: 301000, VMCount: 38,
				HBAs:       []vsphere.HostHBA{{Key: name + "-hba-2", Device: "vmhba1", Bus: 4, Status: "online", Model: "Broadcom 57508", Driver: "bnxtroce", PCI: "0000:af:00.0", StorageProtocol: "iscsi", Type: "HostInternetScsiHba", IScsiName: "iqn.2026-09.example:" + name + ":esxi-02", IScsiAlias: "esxi-02.example.internal"}},
				NICs:       []vsphere.HostNIC{{Key: name + "-pnic-2", Device: "vmnic1", PCI: "0000:19:00.0", Driver: "ixgben", MAC: "00:50:56:aa:02:01", LinkSpeedMB: int32Value(10000), Duplex: boolValue(true), WakeOnLAN: false, Switch: "vSwitch0"}},
				VSwitches:  []vsphere.HostVSwitch{{Key: name + "-switch-2", Name: "vSwitch0", NumPorts: 128, FreePorts: 119, MTU: 1500, Uplinks: []string{"vmnic1"}, Promiscuous: boolValue(false), MACChanges: boolValue(true), ForgedTransmits: boolValue(true), TrafficShaping: boolValue(false)}},
				PortGroups: []vsphere.HostPortGroup{{Key: name + "-port-2", Name: "Management Network", Switch: "vSwitch0", VLAN: 120, Promiscuous: boolValue(false), MACChanges: boolValue(true), ForgedTransmits: boolValue(true)}},
				VMKs:       []vsphere.HostVMKernel{{Key: name + "-vmk-2", Device: "vmk0", PortGroup: "Management Network", MAC: "00:50:56:aa:02:02", MTU: 1500, TSO: boolValue(true), Netstack: "defaultTcpipStack", DHCP: boolValue(false), IP: subnet + ".20", SubnetMask: "255.255.255.0", ServiceConsole: false}},
				Multipaths: []vsphere.HostMultipath{{Key: name + "-lun-2", LUN: "naa.60060160.example.0002", DevicePath: "/vmfs/devices/disks/naa.60060160.example.0002", Policy: "VMW_PSP_RR", PathCount: 2, Active: 2, WorkingPaths: 2}},
			},
		},
		Clusters: []vsphere.Cluster{
			{Location: loc("host", "compute-a"), ID: name + "-cluster-1", Name: "compute-a", Hosts: 4, EffectiveHost: 4, CPUCores: 128, TotalCPUMHz: 307200, TotalMemoryMB: 2097152, DRSEnabled: true, HAEnabled: true},
			{Location: loc("host", "compute-b"), ID: name + "-cluster-2", Name: "compute-b", Hosts: 3, EffectiveHost: 3, CPUCores: 96, TotalCPUMHz: 230400, TotalMemoryMB: 1572864, DRSEnabled: true, HAEnabled: true},
		},
		ResourcePools: []vsphere.ResourcePool{
			{Location: vsphere.Location{Context: name, Datacenter: datacenter, Path: "/" + datacenter + "/host/compute-a/Resources"}, ID: name + "-pool-root-a", Name: "Resources", Root: true, Owner: "compute-a", Status: "green", ConfigStatus: "green", VMRefs: []string{name + "-vm-1", name + "-vm-2"}, CPUReservationMHz: int64Value(0), CPULimitMHz: int64Value(-1), CPUExpandable: true, CPUShares: 4000, CPULevel: "normal", MemConfiguredMB: 49152, MemReservationMB: int64Value(0), MemLimitMB: int64Value(-1), MemExpandable: true, MemShares: 4000, MemLevel: "normal"},
			{Location: vsphere.Location{Context: name, Datacenter: datacenter, Path: "/" + datacenter + "/host/compute-a/Resources/api-pool"}, ID: name + "-pool-1", Name: "api-pool", Parent: "Resources", Owner: "compute-a", Status: "green", ConfigStatus: "green", VMRefs: []string{name + "-vm-1"}, CPUReservationMHz: int64Value(2000), CPULimitMHz: int64Value(16000), CPUExpandable: true, CPUShares: 2000, CPULevel: "normal", MemConfiguredMB: 16384, MemReservationMB: int64Value(4096), MemLimitMB: int64Value(32768), MemExpandable: true, MemShares: 2000, MemLevel: "normal"},
			{Location: vsphere.Location{Context: name, Datacenter: datacenter, Path: "/" + datacenter + "/host/compute-b/Resources"}, ID: name + "-pool-root-b", Name: "Resources", Root: true, Owner: "compute-b", Status: "green", ConfigStatus: "green", VMRefs: []string{name + "-vm-3"}, CPUReservationMHz: int64Value(0), CPULimitMHz: int64Value(-1), CPUExpandable: true, CPUShares: 4000, CPULevel: "normal", MemConfiguredMB: 24576, MemReservationMB: int64Value(0), MemLimitMB: int64Value(-1), MemExpandable: true, MemShares: 4000, MemLevel: "normal"},
		},
		VApps: []vsphere.VApp{
			{
				Location: vappLoc("api-stack"), ID: name + "-vapp-1", Name: "api-stack", Status: "started",
				ParentContainer: "compute-a/Resources", DirectVMCount: 1, DirectVMs: []string{"api-01"},
				DirectVMRefs: []string{"VirtualMachine:" + name + "-vm-1"}, ChildVAppCount: 1,
				ChildVApps: []string{"api-cache"}, ChildVAppRefs: []string{"VirtualApp:" + name + "-vapp-2"}, ChildResourcePoolCount: 1,
				ChildResourcePools: []string{"api-pool"}, ChildResourcePoolRefs: []string{"ResourcePool:" + name + "-pool-1"}, Cluster: "compute-a", ComputeResource: "compute-a",
			},
			{
				Location: vappLoc("api-cache"), ID: name + "-vapp-2", Name: "api-cache", Status: "stopped",
				ParentContainer: "api-stack", ParentVApp: "api-stack", DirectVMCount: 1,
				DirectVMs: []string{"postgres-01"}, DirectVMRefs: []string{"VirtualMachine:" + name + "-vm-2"},
				Cluster: "compute-a", ComputeResource: "compute-a",
			},
			{
				Location: vappLoc("empty-vapp"), ID: name + "-vapp-3", Name: "empty-vapp", Status: "stopped",
				ParentContainer: "compute-a/Resources", Cluster: "compute-a", ComputeResource: "compute-a",
			},
		},
		Datastores: []vsphere.Datastore{
			{Location: loc("datastore", "nvme-01"), ID: name + "-ds-1", Name: "nvme-01", Type: "VMFS", Accessible: true, CapacityBytes: 8 << 40, FreeBytes: 3 << 40},
			{Location: loc("datastore", "san-01"), ID: name + "-ds-2", Name: "san-01", Type: "VMFS", Accessible: true, CapacityBytes: 24 << 40, FreeBytes: 9 << 40},
		},
		DVSwitches: []vsphere.DVSwitch{
			{Location: loc("network", "DVS-Production"), ID: name + "-dvs-1", Name: "DVS-Production", UUID: name + "-dvs-uuid", Vendor: "VMware", Version: "8.0.3", NumPorts: 256, MaxPorts: 4096, MaxMTU: 9000, Hosts: []string{"esxi-01", "esxi-02"}, UplinkPorts: []string{"DVS-Production-DVUplinks"}, LinkDiscoveryProtocol: "lldp", LinkDiscoveryOperation: "both", LACPVersion: "multipleLag", PortGroups: []vsphere.DVPortGroup{
				{ID: name + "-dvpg-1", Key: "dvportgroup-1", Name: "frontend-vlan-120", Switch: "DVS-Production", Type: "earlyBinding", BackingType: "standard", NumPorts: 128, VLAN: "120", Promiscuous: boolValue(false), MACChanges: boolValue(true), ForgedTransmits: boolValue(true), TeamingPolicy: "loadbalance_loadbased", NotifySwitches: boolValue(true), Failback: boolValue(true), IngressShaping: boolValue(false), EgressShaping: boolValue(false), Blocked: boolValue(false), AutoExpand: boolValue(true), ActiveUplinks: []string{"dvUplink1"}},
				{ID: name + "-dvpg-2", Key: "dvportgroup-2", Name: "backend-vlan-240", Switch: "DVS-Production", Type: "earlyBinding", BackingType: "standard", NumPorts: 128, VLAN: "240", Promiscuous: boolValue(false), MACChanges: boolValue(true), ForgedTransmits: boolValue(true), TeamingPolicy: "loadbalance_loadbased", NotifySwitches: boolValue(true), Failback: boolValue(true), ActiveUplinks: []string{"dvUplink1"}},
			}},
		},
		Networks: []vsphere.Network{
			{Location: loc("network", "frontend-vlan-120"), ID: name + "-net-1", Name: "frontend-vlan-120", Type: "DistributedVirtualPortgroup", Switch: "DVS-Production", VLAN: "120", Accessible: true},
			{Location: loc("network", "backend-vlan-240"), ID: name + "-net-2", Name: "backend-vlan-240", Type: "DistributedVirtualPortgroup", Switch: "DVS-Production", VLAN: "240", Accessible: true},
		},
	}
}

func healthyDiagnosis(cc *config.Context, latency time.Duration) *vsphere.Diagnosis {
	return &vsphere.Diagnosis{
		Context: cc.Name, Endpoint: cc.Endpoint, Route: cc.Transport.Describe(), TLS: cc.TLS.Describe(), Latency: latency,
		Checks: []vsphere.Check{
			{Name: "Configuration valid", Status: vsphere.CheckPass, Detail: cc.Endpoint},
			{Name: "Credential available", Status: vsphere.CheckPass, Detail: "keyring:" + cc.Name},
			{Name: "Route configured", Status: vsphere.CheckPass, Detail: cc.Transport.Describe()},
			{Name: "DNS resolution", Status: vsphere.CheckPass, Detail: "10.20.0.15"},
			{Name: "TCP connection", Status: vsphere.CheckPass, Detail: "connected"},
			{Name: "TLS handshake", Status: vsphere.CheckPass, Detail: "certificate verified"},
			{Name: "Authentication", Status: vsphere.CheckPass, Detail: "VMware vCenter Server 8.0.3"},
			{Name: "API access", Status: vsphere.CheckPass, Detail: "inventory readable"},
		},
	}
}

func failedDiagnosis(cc *config.Context) *vsphere.Diagnosis {
	failure := errors.New("dial tcp 10.24.0.8:3128: connection refused")
	return &vsphere.Diagnosis{
		Context: cc.Name, Endpoint: cc.Endpoint, Route: cc.Transport.Describe(), TLS: cc.TLS.Describe(),
		Checks: []vsphere.Check{
			{Name: "Configuration valid", Status: vsphere.CheckPass, Detail: cc.Endpoint},
			{Name: "Credential available", Status: vsphere.CheckPass, Detail: "keyring:" + cc.Name},
			{Name: "Route configured", Status: vsphere.CheckPass, Detail: cc.Transport.Describe()},
			{Name: "Proxy reachable", Status: vsphere.CheckFail, Err: failure},
			{Name: "DNS resolution", Status: vsphere.CheckSkip},
			{Name: "TCP connection", Status: vsphere.CheckSkip},
			{Name: "TLS handshake", Status: vsphere.CheckSkip},
			{Name: "Authentication", Status: vsphere.CheckSkip},
			{Name: "API access", Status: vsphere.CheckSkip},
		},
	}
}
