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

// Provisioned MiB is in use plus uncommitted space, so a 2 TiB datastore
// with 1 TiB free and 2 TiB uncommitted has provisioned 3 TiB.
func TestWriteRVToolsVDatastoreProvisioned(t *testing.T) {
	uncommitted := int64(2 << 40)
	for _, tc := range []struct {
		name        string
		uncommitted *int64
		want        string
	}{
		{"recorded", &uncommitted, "3145728"},
		{"not recorded", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			payload, err := json.Marshal(vsphere.Datastore{ID: "ds-1", Name: "datastore-1", Accessible: true, CapacityBytes: 2 << 40, FreeBytes: 1 << 40, UncommittedBytes: tc.uncommitted})
			if err != nil {
				t.Fatal(err)
			}
			for i := range data.Resources {
				if data.Resources[i].Kind == "datastore" {
					data.Resources[i].Payload = payload
				}
			}
			if got := firstRowByHeader(t, data, "vDatastore")["Provisioned MiB"]; got != tc.want {
				t.Errorf("Provisioned MiB = %q, want %q", got, tc.want)
			}
		})
	}
}

// The columns vsfleet writes keep RVTools' relative order, so a pipeline
// reading by position between shared columns sees the same layout. vInfo
// and the other VM sheets are the full headers of the RVTools 4.8.2.1 export
// of the nested lab.
func TestCapturedColumnsFollowRVToolsOrder(t *testing.T) {
	for _, tc := range []struct {
		sheet   string
		headers []string
		rvtools []string
	}{
		{"vInfo", vmHeaders, []string{"VM", "Powerstate", "Template", "SRM Placeholder", "Config status", "DNS Name", "Connection state",
			"Guest state", "Heartbeat", "Consolidation Needed", "PowerOn", "Suspended To Memory", "Suspend time", "Suspend Interval",
			"Creation date", "Change Version", "CPUs", "Overall Cpu Readiness", "Memory", "Active Memory", "NICs", "Disks",
			"Total disk capacity MiB", "Fixed Passthru HotPlug", "min Required EVC Mode Key", "Latency Sensitivity",
			"Op Notification Timeout", "EnableUUID", "CBT", "Primary IP Address", "Network #1", "Network #2", "Network #3", "Network #4",
			"Network #5", "Network #6", "Network #7", "Network #8", "Num Monitors", "Video Ram KiB", "Resource pool", "Folder ID",
			"Folder", "vApp", "DAS protection", "FT State", "FT Role", "FT Latency", "FT Bandwidth", "FT Sec. Latency",
			"Vm Failover In Progress", "Provisioned MiB", "In Use MiB", "Unshared MiB", "HA Restart Priority", "HA Isolation Response",
			"HA VM Monitoring", "Cluster rule(s)", "Cluster rule name(s)", "Boot Required", "Boot delay", "Boot retry delay",
			"Boot retry enabled", "Boot BIOS setup", "Reboot PowerOff", "EFI Secure boot", "Firmware", "HW version", "HW upgrade status",
			"HW upgrade policy", "HW target", "Path", "Log directory", "Snapshot directory", "Suspend directory", "Annotation",
			"Datacenter", "Cluster", "Host", "OS according to the configuration file", "OS according to the VMware Tools",
			"Customization Info", "Guest Detailed Data", "VM ID", "SMBIOS UUID", "VM UUID", "VI SDK Server type", "VI SDK API Version",
			"VI SDK Server", "VI SDK UUID"}},
		{"vCPU", cpuHeaders, []string{"VM", "Powerstate", "Template", "SRM Placeholder", "CPUs", "Sockets", "Cores p/s", "Max", "Overall", "Level",
			"Shares", "Reservation", "Entitlement", "DRS Entitlement", "Limit", "Hot Add", "Hot Remove", "Numa Hotadd Exposed", "Annotation",
			"Datacenter", "Cluster", "Host", "Folder", "OS according to the configuration file", "OS according to the VMware Tools", "VM ID",
			"VM UUID", "VI SDK Server", "VI SDK UUID"}},
		{"vMemory", memoryHeaders, []string{"VM", "Powerstate", "Template", "SRM Placeholder", "Size MiB", "Memory Reservation Locked To Max",
			"Overhead", "Max", "Consumed", "Consumed Overhead", "Private", "Shared", "Swapped", "Ballooned", "Active", "Entitlement",
			"DRS Entitlement", "Level", "Shares", "Reservation", "Limit", "Hot Add", "Annotation", "Datacenter", "Cluster", "Host", "Folder",
			"OS according to the configuration file", "OS according to the VMware Tools", "VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID"}},
		{"vTools", toolsHeaders, []string{"VM", "Powerstate", "Template", "SRM Placeholder", "VM Version", "Tools", "Tools Version",
			"Required Version", "Upgradeable", "Upgrade Policy", "Sync time", "App status", "Heartbeat status", "Kernel Crash state",
			"Operation Ready", "State change support", "Interactive Guest", "Annotation", "Datacenter", "Cluster", "Host", "Folder",
			"OS according to the configuration file", "OS according to the VMware Tools", "VMRef", "VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID"}},
		{"vUSB", usbHeaders, []string{"VM", "Powerstate", "Template", "SRM Placeholder", "Device Node", "Device Type", "Connected", "Family",
			"Speed", "EHCI enabled", "Auto connect", "Bus number", "Unit number", "Annotation", "Datacenter", "Cluster", "Host", "Folder",
			"OS according to the configuration file", "OS according to the VMware tools", "VMRef", "VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID"}},
		{"vCD", cdHeaders, []string{"VM", "Powerstate", "Template", "SRM Placeholder", "Device Node", "Connected", "Starts Connected", "Device Type",
			"Annotation", "Datacenter", "Cluster", "Host", "Folder", "OS according to the configuration file", "OS according to the VMware Tools",
			"VMRef", "VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID"}},
		{"vNetwork", networkHeaders, []string{"VM", "Powerstate", "Template", "SRM Placeholder", "NIC label", "Adapter", "Network", "Switch",
			"Connected", "Starts Connected", "Mac Address", "Type", "IPv4 Address", "IPv6 Address", "Direct Path IO", "Internal Sort Column",
			"Annotation", "Datacenter", "Cluster", "Host", "Folder", "OS according to the configuration file", "OS according to the VMware Tools",
			"VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID"}},
		{"vDisk", diskHeaders, []string{"VM", "Powerstate", "Template", "SRM Placeholder", "Disk", "Disk Key", "Disk UUID", "Disk Path",
			"Capacity MiB", "Raw", "Disk Mode", "Sharing mode", "Thin", "Eagerly Scrub", "Split", "Write Through", "Level", "Shares", "Reservation",
			"Limit", "Controller", "Label", "SCSI Unit #", "Unit #", "Shared Bus", "Path", "Raw LUN ID", "Raw Comp. Mode", "Internal Sort Column",
			"Annotation", "Datacenter", "Cluster", "Host", "Folder", "OS according to the configuration file", "OS according to the VMware Tools",
			"VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID"}},
		{"vSnapshot", snapshotHeaders, []string{"VM", "Powerstate", "Name", "Description", "Date / time", "Filename", "Size MiB (vmsn)",
			"Size MiB (total)", "Quiesced", "State", "Annotation", "Datacenter", "Cluster", "Host", "Folder", "OS according to the configuration file",
			"OS according to the VMware Tools", "VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID"}},
		{"vPartition", partitionHeaders, []string{"VM", "Powerstate", "Template", "SRM Placeholder", "Disk Key", "Disk", "Capacity MiB",
			"Consumed MiB", "Free MiB", "Free %", "Internal Sort Column", "Annotation", "Datacenter", "Cluster", "Host", "Folder",
			"OS according to the configuration file", "OS according to the VMware Tools", "VM ID", "VM UUID", "VI SDK Server", "VI SDK UUID"}},
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
		{"vHBA", hbaHeaders, []string{"Host", "Datacenter", "Cluster", "Device", "Type", "Status", "Bus", "Pci", "Driver", "Model", "WWN",
			"VI SDK Server", "VI SDK UUID"}},
		{"vNIC", nicHeaders, []string{"Host", "Datacenter", "Cluster", "Network Device", "Driver", "Speed", "Duplex", "MAC", "Switch",
			"Uplink port", "PCI", "WakeOn", "VI SDK Server", "VI SDK UUID"}},
		{"vRP", resourcePoolHeaders, []string{"Resource Pool name", "Resource Pool path", "Status", "# VMs total", "# VMs", "# vCPUs",
			"CPU limit", "CPU overheadLimit", "CPU reservation", "CPU level", "CPU shares", "CPU expandableReservation", "CPU maxUsage",
			"CPU overallUsage", "CPU reservationUsed", "CPU reservationUsedForVm", "CPU unreservedForPool", "CPU unreservedForVm", "Mem Configured",
			"Mem limit", "Mem overheadLimit", "Mem reservation", "Mem level", "Mem shares", "Mem expandableReservation", "Mem maxUsage",
			"Object ID", "VI SDK Server", "VI SDK UUID"}},
		{"vSwitch", switchHeaders, []string{"Host", "Datacenter", "Cluster", "Switch", "# Ports", "Free Ports", "Promiscuous Mode", "Mac Changes",
			"Forged Transmits", "Traffic Shaping", "Width", "Peak", "Burst", "Policy", "Reverse Policy", "Notify Switch", "Rolling Order", "Offload",
			"TSO", "Zero Copy Xmit", "MTU", "VI SDK Server", "VI SDK UUID"}},
		{"vPort", portHeaders, []string{"Host", "Datacenter", "Cluster", "Port Group", "Switch", "VLAN", "Promiscuous Mode", "Mac Changes",
			"Forged Transmits", "Traffic Shaping", "Width", "Peak", "Burst", "Policy", "Reverse Policy", "Notify Switch", "Rolling Order", "Offload",
			"TSO", "Zero Copy Xmit", "VI SDK Server", "VI SDK UUID"}},
		{"dvSwitch", dvSwitchHeaders, []string{"Switch", "Datacenter", "Name", "Vendor", "Version", "Description", "Created", "Host members",
			"Max Ports", "# Ports", "# VMs", "In Traffic Shaping", "In Avg", "In Peak", "In Burst", "Out Traffic Shaping", "Out Avg", "Out Peak",
			"Out Burst", "CDP Type", "CDP Operation", "LACP Name", "LACP Mode", "LACP Load Balance Alg.", "Max MTU", "Contact", "Admin Name",
			"Object ID", "VI SDK Server", "VI SDK UUID"}},
		{"dvPort", dvPortHeaders, []string{"Port", "Switch", "Type", "# Ports", "VLAN", "Speed", "Full Duplex", "Blocked", "Allow Promiscuous",
			"Mac Changes", "Active Uplink", "Standby Uplink", "Policy", "Forged Transmits", "In Traffic Shaping", "In Avg", "In Peak", "In Burst",
			"Out Traffic Shaping", "Out Avg", "Out Peak", "Out Burst", "Reverse Policy", "Notify Switch", "Rolling Order", "Check Beacon",
			"Live Port Moving", "Check Duplex", "Check Error %", "Check Speed", "Percentage", "Block Override", "Config Reset", "Shaping Override",
			"Vendor Config Override", "Sec. Policy Override", "Teaming Override", "Vlan Override", "Object ID", "VI SDK Server", "VI SDK UUID"}},
		{"vSC_VMK", vmkHeaders, []string{"Host", "Datacenter", "Cluster", "Port Group", "Device", "Mac Address", "DHCP", "IP Address",
			"IP 6 Address", "Subnet mask", "Gateway", "IP 6 Gateway", "MTU", "VI SDK Server", "VI SDK UUID"}},
		{"vDatastore", datastoreHeaders, []string{"Name", "Config status", "Address", "Accessible", "Type", "# VMs total", "# VMs", "Capacity MiB", "Provisioned MiB", "In Use MiB", "Free MiB", "Free %", "SIOC enabled", "SIOC Threshold", "# Hosts", "Hosts", "Cluster name", "Cluster capacity MiB", "Cluster free space MiB", "Block size", "Max Blocks", "# Extents", "Major Version", "Version", "VMFS Upgradeable", "MHA", "URL", "Object ID", "VI SDK Server", "VI SDK UUID"}},
		{"vMultiPath", multipathHeaders, []string{"Host", "Cluster", "Datacenter", "Datastore", "Disk", "Display name", "Policy", "Oper. State", "Path 1", "Path 1 state", "Path 2", "Path 2 state", "Path 3", "Path 3 state", "Path 4", "Path 4 state", "Path 5", "Path 5 state", "Path 6", "Path 6 state", "Path 7", "Path 7 state", "Path 8", "Path 8 state", "vStorage", "Queue depth", "Vendor", "Model", "Revision", "Level", "Serial #", "UUID", "Object ID", "VI SDK Server", "VI SDK UUID"}},
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
