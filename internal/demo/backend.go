// Package demo provides deterministic, synthetic vCenter data for recording
// and presenting the terminal interface. It never opens a network connection
// or reads configuration and credentials from the operator's machine.
package demo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/contextops"
	"github.com/easonliuuuuu/vsfleet/internal/session"
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

	return &Backend{
		contexts: contexts,
		inventories: map[string]*vsphere.Inventory{
			"prod-vc": sampleInventory("prod-vc", "Taipei", "10.20.0"),
			"edge-vc": sampleInventory("edge-vc", "Hsinchu", "10.42.0"),
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

// Inventory implements tui.Backend.
func (b *Backend) Inventory(_ context.Context, cc *config.Context) (*vsphere.Inventory, error) {
	if err := b.failures[cc.Name]; err != nil {
		return nil, err
	}
	inv, ok := b.inventories[cc.Name]
	if !ok {
		return nil, fmt.Errorf("demo inventory for %q not found", cc.Name)
	}
	return inv, nil
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
			{Location: loc("host", "esxi-01"), ID: name + "-host-1", Name: "esxi-01", Cluster: "compute-a", PowerState: "poweredOn", ConnectionState: "connected", Vendor: "Dell Inc.", Model: "PowerEdge R750", Version: "8.0.3", Build: "24022515", CPUCores: 32, CPUThreads: 64, CPUMHz: 2400, MemoryMB: 524288, CPUUsageMHz: 18400, MemoryUsageMB: 244000, VMCount: 31},
			{Location: loc("host", "esxi-02"), ID: name + "-host-2", Name: "esxi-02", Cluster: "compute-a", PowerState: "poweredOn", ConnectionState: "connected", Vendor: "Dell Inc.", Model: "PowerEdge R750", Version: "8.0.3", Build: "24022515", CPUCores: 32, CPUThreads: 64, CPUMHz: 2400, MemoryMB: 524288, CPUUsageMHz: 22100, MemoryUsageMB: 301000, VMCount: 38},
		},
		Clusters: []vsphere.Cluster{
			{Location: loc("host", "compute-a"), ID: name + "-cluster-1", Name: "compute-a", Hosts: 4, EffectiveHost: 4, CPUCores: 128, TotalCPUMHz: 307200, TotalMemoryMB: 2097152, DRSEnabled: true, HAEnabled: true},
			{Location: loc("host", "compute-b"), ID: name + "-cluster-2", Name: "compute-b", Hosts: 3, EffectiveHost: 3, CPUCores: 96, TotalCPUMHz: 230400, TotalMemoryMB: 1572864, DRSEnabled: true, HAEnabled: true},
		},
		Datastores: []vsphere.Datastore{
			{Location: loc("datastore", "nvme-01"), ID: name + "-ds-1", Name: "nvme-01", Type: "VMFS", Accessible: true, CapacityBytes: 8 << 40, FreeBytes: 3 << 40},
			{Location: loc("datastore", "san-01"), ID: name + "-ds-2", Name: "san-01", Type: "VMFS", Accessible: true, CapacityBytes: 24 << 40, FreeBytes: 9 << 40},
		},
		Networks: []vsphere.Network{
			{Location: loc("network", "frontend-vlan-120"), ID: name + "-net-1", Name: "frontend-vlan-120", Type: "DistributedVirtualPortgroup", Accessible: true},
			{Location: loc("network", "backend-vlan-240"), ID: name + "-net-2", Name: "backend-vlan-240", Type: "DistributedVirtualPortgroup", Accessible: true},
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
