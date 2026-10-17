package report

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The "lab" row is what the real RVTools 4.8.1.4 export of the nested lab
// writes for 192.168.150.11, which runs only vcsa (2 vCPUs, 14336 MiB).
func TestWriteRVToolsVHostDetails(t *testing.T) {
	no, yes := false, true
	zero := int32(0)
	boot := time.Date(2026, 10, 4, 13, 24, 29, 0, time.UTC)
	lab := &vsphere.HostDetails{
		ProductName: "VMware ESXi 8.0.3 build-24784735", CPUModel: "13th Gen Intel(R) Core(TM) i5-13500", CPUPackages: 4,
		HTAvailable: &no, HTActive: &no, VMotion: &yes, StorageVMotion: &yes, BootTime: &boot, DNSDHCP: &no, Domain: "lab.local",
		NTPDRunning: &no, TimeZone: "UTC", TimeZoneName: "UTC", GMTOffset: &zero, BIOSVendor: "Ubuntu distribution of EDK II",
		BIOSVersion: "2025.11-3ubuntu7.2", UUID: "b1170caf-31ff-4d8f-bf59-a480f8bb6844",
	}
	labCells := map[string]string{
		"CPU Model": "13th Gen Intel(R) Core(TM) i5-13500", "Speed": "2496", "HT Available": "FALSE", "HT Active": "FALSE", "# CPU": "4",
		"Cores per CPU": "1", "# Cores": "4", "# VMs": "1", "VMs per Core": "0.25", "# vCPUs": "2", "vCPUs per Core": "0.5", "vRAM": "14336",
		"VMotion support": "TRUE", "Storage VMotion support": "TRUE", "Current EVC": "", "Max EVC": "", "ESX Version": "VMware ESXi 8.0.3 build-24784735",
		"DNS Servers": "", "DHCP": "FALSE", "Domain": "lab.local", "NTP Server(s)": "", "NTPD running": "FALSE", "Time Zone": "UTC",
		"Time Zone Name": "UTC", "GMT Offset": "0", "Serial number": "", "BIOS Vendor": "Ubuntu distribution of EDK II",
		"BIOS Version": "2025.11-3ubuntu7.2", "Certificate Issuer": "", "UUID": "b1170caf-31ff-4d8f-bf59-a480f8bb6844",
	}
	detailCells := []string{"CPU Model", "HT Available", "HT Active", "# CPU", "Cores per CPU", "VMotion support", "Storage VMotion support",
		"Boot time", "DHCP", "Domain", "NTPD running", "Time Zone", "Time Zone Name", "GMT Offset", "BIOS Vendor", "BIOS Version", "UUID"}
	older := map[string]string{"ESX Version": "8.0.3", "# VMs": "1"}
	for _, c := range detailCells {
		older[c] = ""
	}
	for _, tc := range []struct {
		name     string
		details  *vsphere.HostDetails
		vmStatus string
		want     map[string]string
	}{
		{"lab", lab, "success", labCells},
		{"captured before schema 26", nil, "success", older},
		{"VM collection failed", lab, "failed",
			map[string]string{"# VMs": "", "VMs per Core": "", "# vCPUs": "", "vCPUs per Core": "", "vRAM": "", "CPU Model": "13th Gen Intel(R) Core(TM) i5-13500"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			data.Contexts[0].VMStatus = tc.vmStatus
			app := &data.VMs[0].Observation.VM
			app.CPU, app.MemoryMB = 2, 14336
			// Neither a template nor a powered-off VM counts towards density.
			for _, vm := range []vsphere.VM{{ID: "vm-2", Name: "tpl", Host: "esx-1", PowerState: "poweredOff", IsTemplate: true, CPU: 4},
				{ID: "vm-3", Name: "off", Host: "esx-1", PowerState: "poweredOff", CPU: 4}} {
				data.VMs = append(data.VMs, assessment.ExportVM{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-uuid", VM: vm}})
			}
			payload, err := json.Marshal(vsphere.Host{ID: "host-1", Name: "esx-1", CPUCores: 4, CPUMHz: 2496, Version: "8.0.3", Details: tc.details})
			if err != nil {
				t.Fatal(err)
			}
			for i := range data.Resources {
				if data.Resources[i].Kind == "host" {
					data.Resources[i].Payload = payload
				}
			}
			got := firstRowByHeader(t, data, "vHost")
			for header, want := range tc.want {
				if cell, found := got[header]; !found || cell != want {
					t.Errorf("%s = %q (column present: %v), want %q", header, cell, found, want)
				}
			}
		})
	}
}

// The lab RVTools 4.8.1.4 export writes 2 NICs and 1 HBA on each host. An
// empty adapter list is a count only when the host's configuration was read.
func TestWriteRVToolsVHostAdapterCounts(t *testing.T) {
	nics := []vsphere.HostNIC{{Key: "nic-0", Device: "vmnic0"}, {Key: "nic-1", Device: "vmnic1"}}
	hbas := []vsphere.HostHBA{{Key: "hba-0", Device: "vmhba0"}}
	vmks := []vsphere.HostVMKernel{{Key: "vmk-0", Device: "vmk0"}}
	for _, tc := range []struct {
		name   string
		schema string
		host   vsphere.Host
		nics   string
		hbas   string
	}{
		{"lab", assessment.CurrentInventorySchemaVersion, vsphere.Host{NICs: nics, HBAs: hbas, VMKs: vmks}, "2", "1"},
		{"configuration read, no adapters", assessment.CurrentInventorySchemaVersion, vsphere.Host{VMKs: vmks}, "0", "0"},
		{"disconnected host", assessment.CurrentInventorySchemaVersion, vsphere.Host{ConnectionState: "disconnected"}, "", ""},
		// An import claims schema 5 at most, and a workbook missing vHBA would
		// otherwise report every host as having none.
		{"RVTools import", "5", vsphere.Host{NICs: nics, VMKs: vmks}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			data.Run.InventorySchemaVersion = tc.schema
			tc.host.ID, tc.host.Name = "host-1", "esx-1"
			payload, err := json.Marshal(tc.host)
			if err != nil {
				t.Fatal(err)
			}
			for i := range data.Resources {
				if data.Resources[i].Kind == "host" {
					data.Resources[i].Payload = payload
				}
			}
			got := firstRowByHeader(t, data, "vHost")
			if got["# NICs"] != tc.nics || got["# HBAs"] != tc.hbas {
				t.Errorf("# NICs, # HBAs = %q, %q, want %q, %q", got["# NICs"], got["# HBAs"], tc.nics, tc.hbas)
			}
		})
	}
}
