package report

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The "lab" rows are what the real RVTools 4.8.1.4 export of the nested lab
// writes for vm-reservations; the summary-only VM is one vSphere returned no
// configuration for.
func TestWriteRVToolsVMCapturedColumns(t *testing.T) {
	no := false
	cpuReservation, cpuLimit, memReservation, memLimit := int64(500), int64(1000), int64(128), int64(256)
	lab := func(vm *vsphere.VM) {
		vm.ConfigurationAvailable = true
		vm.CPU, vm.CPUSockets, vm.CoresPerSocket = 1, 1, 1
		vm.CPUAllocation = &vsphere.VMResourceAllocation{Reservation: &cpuReservation, Limit: &cpuLimit}
		vm.MemoryAllocation = &vsphere.VMResourceAllocation{Reservation: &memReservation, Limit: &memLimit}
		vm.MemoryReservationLockedToMax = &no
		vm.Disks = []vsphere.VMDisk{{Key: 2000, CapacityBytes: 1 << 30}}
		vm.NICs = []vsphere.VMNIC{{Key: 4000, Network: "dvpg-mgmt"}}
	}
	nineNICs := func(vm *vsphere.VM) {
		lab(vm)
		vm.Disks = []vsphere.VMDisk{{Key: 2000, CapacityBytes: 1 << 30}, {Key: 2001, CapacityBytes: 512 << 20}}
		vm.NICs = nil
		for _, network := range []string{"n1", "", "n3", "n4", "n5", "n6", "n7", "n8", "n9"} {
			vm.NICs = append(vm.NICs, vsphere.VMNIC{Network: network})
		}
	}
	summaryOnly := func(vm *vsphere.VM) {
		vm.ConfigurationAvailable, vm.Disks, vm.NICs = false, nil, nil
	}
	for _, tc := range []struct {
		name   string
		mutate func(*vsphere.VM)
		want   map[string]map[string]string
	}{
		{"lab", lab, map[string]map[string]string{
			"vInfo":   {"Total disk capacity MiB": "1024", "Network #1": "dvpg-mgmt", "Network #2": "", "Network #8": ""},
			"vCPU":    {"Sockets": "1", "Cores p/s": "1", "Reservation": "500", "Limit": "1000"},
			"vMemory": {"Memory Reservation Locked To Max": "FALSE", "Reservation": "128", "Limit": "256"},
		}},
		{"more NICs than slots", nineNICs, map[string]map[string]string{
			"vInfo": {"Total disk capacity MiB": "1536", "Network #1": "n1", "Network #2": "", "Network #3": "n3", "Network #8": "n8"},
		}},
		{"summary only", summaryOnly, map[string]map[string]string{
			"vInfo":   {"Total disk capacity MiB": "", "Network #1": ""},
			"vCPU":    {"Sockets": "", "Cores p/s": "", "Reservation": "", "Limit": ""},
			"vMemory": {"Memory Reservation Locked To Max": "", "Reservation": "", "Limit": ""},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			tc.mutate(&data.VMs[0].Observation.VM)
			for sheet, cells := range tc.want {
				got := firstRowByHeader(t, data, sheet)
				for header, want := range cells {
					if cell, found := got[header]; !found || cell != want {
						t.Errorf("%s/%s = %q (column present: %v), want %q", sheet, header, cell, found, want)
					}
				}
			}
		})
	}
}

// The "DRS on" and "DRS off" rows are Cluster-DR and Cluster-Lab in the real
// RVTools 4.8.1.4 export of the nested lab. RVTools also writes
// fullyAutomated for Cluster-Lab, a documented difference.
func TestWriteRVToolsVClusterEffectiveCapacity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cluster vsphere.Cluster
		want    map[string]string
	}{
		{"DRS on", vsphere.Cluster{ID: "domain-c2005", Name: "Cluster-DR", Hosts: 1, EffectiveHost: 1, EffectiveCPUMHz: 7263, EffectiveMemoryMB: 5460, DRSEnabled: true, DRSBehavior: "fullyAutomated"},
			map[string]string{"Effective Cpu": "7263", "Effective Memory": "5460", "DRS default VM behavior": "fullyAutomated"}},
		{"DRS off", vsphere.Cluster{ID: "domain-c9", Name: "Cluster-Lab", Hosts: 1, EffectiveHost: 1, EffectiveCPUMHz: 6888, EffectiveMemoryMB: 15158},
			map[string]string{"Effective Cpu": "6888", "Effective Memory": "15158", "DRS default VM behavior": ""}},
		{"no host can run VMs", vsphere.Cluster{ID: "domain-c10", Name: "all-in-maintenance", Hosts: 2},
			map[string]string{"Effective Cpu": "0", "Effective Memory": "0"}},
		{"not recorded", vsphere.Cluster{ID: "domain-c11", Name: "old", Hosts: 1, EffectiveHost: 1},
			map[string]string{"Effective Cpu": "", "Effective Memory": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			payload, err := json.Marshal(tc.cluster)
			if err != nil {
				t.Fatal(err)
			}
			data.Resources = append(data.Resources, assessment.ResourceObservation{Context: "prod", VCenterID: "vc-uuid", Kind: "cluster", ID: tc.cluster.ID, Name: tc.cluster.Name, Payload: payload})
			got := firstRowByHeader(t, data, "vCluster")
			for header, want := range tc.want {
				if cell, found := got[header]; !found || cell != want {
					t.Errorf("%s = %q (column present: %v), want %q", header, cell, found, want)
				}
			}
		})
	}
}

// The URL is local-esxi01's in the real RVTools 4.8.1.4 export of the lab.
func TestWriteRVToolsVDatastoreURL(t *testing.T) {
	for _, tc := range []struct{ name, url string }{
		{"lab", "ds:///vmfs/volumes/6ab8f549-3dc1bcad-5f72-525400865436/"},
		{"not recorded", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			payload, err := json.Marshal(vsphere.Datastore{ID: "ds-1", Name: "datastore-1", Backing: vsphere.DatastoreBacking{URL: tc.url}})
			if err != nil {
				t.Fatal(err)
			}
			for i := range data.Resources {
				if data.Resources[i].Kind == "datastore" {
					data.Resources[i].Payload = payload
				}
			}
			if got := firstRowByHeader(t, data, "vDatastore")["URL"]; got != tc.url {
				t.Errorf("URL = %q, want %q", got, tc.url)
			}
		})
	}
}

// The columns vsfleet writes keep RVTools 4.8.1.4's relative order, so a
// pipeline reading by position between shared columns sees the same layout.
func TestCapturedColumnsFollowRVToolsOrder(t *testing.T) {
	for _, tc := range []struct {
		sheet   string
		headers []string
		rvtools []string
	}{
		{"vInfo", vmHeaders, []string{"VM", "Powerstate", "Template", "DNS Name", "Connection state", "Guest state", "CPUs", "Memory", "NICs", "Disks",
			"Total disk capacity MiB", "Primary IP Address", "Network #1", "Network #2", "Network #3", "Network #4", "Network #5", "Network #6",
			"Network #7", "Network #8", "Resource pool", "Folder", "vApp", "In Use MiB", "EFI Secure boot", "Firmware", "HW version",
			"HW upgrade status", "HW upgrade policy", "HW target", "Annotation", "Datacenter", "Cluster", "Host",
			"OS according to the configuration file", "VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID"}},
		{"vCPU", cpuHeaders, []string{"VM", "Powerstate", "Template", "CPUs", "Sockets", "Cores p/s", "Reservation", "Limit", "Annotation",
			"Datacenter", "Cluster", "Host", "Folder", "OS according to the configuration file", "VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID"}},
		{"vMemory", memoryHeaders, []string{"VM", "Powerstate", "Template", "Size MiB", "Memory Reservation Locked To Max", "Reservation", "Limit",
			"Annotation", "Datacenter", "Cluster", "Host", "Folder", "OS according to the configuration file", "VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID"}},
		{"vHost", hostHeaders, []string{"Host", "Datacenter", "Cluster", "Config status", "Compliance Check State", "in Maintenance Mode",
			"in Quarantine Mode", "vSAN Fault Domain Name", "CPU Model", "Speed", "HT Available", "HT Active", "# CPU", "Cores per CPU", "# Cores",
			"CPU usage %", "# Memory", "Memory Tiering Type", "Memory usage %", "Console", "# NICs", "# HBAs", "# VMs total", "# VMs",
			"VMs per Core", "# vCPUs", "vCPUs per Core", "vRAM", "VM Used memory", "VM Memory Swapped", "VM Memory Ballooned",
			"VMotion support", "Storage VMotion support", "Current EVC", "Max EVC", "Assigned License(s)", "ATS Heartbeat", "ATS Locking",
			"Current CPU power man. policy", "Supported CPU power man.", "Host Power Policy", "ESX Version", "Boot time", "DNS Servers",
			"DHCP", "Domain", "Domain List", "DNS Search Order", "NTP Server(s)", "NTPD running", "Time Zone", "Time Zone Name",
			"GMT Offset", "Vendor", "Model", "Serial number", "Service tag", "OEM specific string", "BIOS Vendor", "BIOS Version",
			"BIOS Date", "Certificate Issuer", "Certificate Start Date", "Certificate Expiry Date", "Certificate Status",
			"Certificate Subject", "Object ID", "AutoDeploy.MachineIdentity", "UUID", "VI SDK Server", "VI SDK UUID"}},
		{"vDatastore", datastoreHeaders, []string{"Name", "Capacity MiB", "In Use MiB", "Free MiB", "Free %", "URL", "Object ID", "VI SDK Server", "VI SDK UUID"}},
	} {
		t.Run(tc.sheet, func(t *testing.T) {
			position := map[string]int{}
			for i, h := range tc.rvtools {
				position[h] = i
			}
			last := -1
			for _, h := range tc.headers {
				p, ok := position[h]
				if !ok {
					continue
				}
				if p < last {
					t.Errorf("%s is out of RVTools order", h)
				}
				last = p
			}
		})
	}
}
