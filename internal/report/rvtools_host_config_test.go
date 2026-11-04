package report

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// writeHostSheet renders one sheet of an export whose only host is h.
func writeHostSheet(t *testing.T, h vsphere.Host, sheet string) map[string]string {
	t.Helper()
	data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	h.ID, h.Name = "host-1", "esx-1"
	payload, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	for i := range data.Resources {
		if data.Resources[i].Kind == "host" {
			data.Resources[i].Payload = payload
		}
	}
	return firstRowByHeader(t, data, sheet)
}

func checkCells(t *testing.T, got, want map[string]string) {
	t.Helper()
	for header, w := range want {
		if cell, found := got[header]; !found || cell != w {
			t.Errorf("%s = %q (column present: %v), want %q", header, cell, found, w)
		}
	}
}

// The "lab" values are what the real RVTools 4.8.2.1 export of the nested
// lab writes for 192.168.150.11.
func TestWriteRVToolsVHostConfiguration(t *testing.T) {
	no := false
	one := int64(1)
	lab := &vsphere.HostDetails{ConfigStatus: "yellow", InQuarantine: &no, MemoryTiering: "noTiering", ATSHeartbeat: &one, ATSLocking: &one,
		CPUPowerPolicy: "Balanced", HostPowerPolicy: "dynamic"}
	full := &vsphere.HostDetails{DomainList: []string{"CORP", "LAB"}, SearchDomains: []string{"lab.local", "corp.local"},
		ServiceTag: "7XK2Q3", OEMString: "Dell System, 5[0000]", CPUPowerSupport: "ACPI P-states"}
	autoDeploy := vsphere.Metadata{CustomAttributes: []vsphere.CustomAttribute{{Name: "AutoDeploy.MachineIdentity", Value: "mac=00:50:56:aa:bb:cc"}}}
	newColumns := []string{"Config status", "in Quarantine Mode", "Memory Tiering Type", "ATS Heartbeat", "ATS Locking",
		"Current CPU power man. policy", "Supported CPU power man.", "Host Power Policy", "Domain List", "DNS Search Order", "Service tag",
		"OEM specific string", "AutoDeploy.MachineIdentity"}
	older := map[string]string{}
	for _, c := range newColumns {
		older[c] = ""
	}
	for _, tc := range []struct {
		name string
		host vsphere.Host
		want map[string]string
	}{
		{"lab", vsphere.Host{Details: lab}, map[string]string{"Config status": "yellow", "in Quarantine Mode": "FALSE",
			"Memory Tiering Type": "noTiering", "ATS Heartbeat": "1", "ATS Locking": "1", "Current CPU power man. policy": "Balanced",
			"Supported CPU power man.": "", "Host Power Policy": "dynamic", "Domain List": "", "DNS Search Order": "", "Service tag": "",
			"OEM specific string": "", "AutoDeploy.MachineIdentity": ""}},
		{"lists and identifiers", vsphere.Host{Details: full, Metadata: autoDeploy}, map[string]string{"Domain List": "CORP, LAB",
			"DNS Search Order": "lab.local, corp.local", "Service tag": "7XK2Q3", "OEM specific string": "Dell System, 5[0000]",
			"Supported CPU power man.": "ACPI P-states", "AutoDeploy.MachineIdentity": "mac=00:50:56:aa:bb:cc", "in Quarantine Mode": "",
			"ATS Heartbeat": ""}},
		{"captured before schema 30", vsphere.Host{Details: &vsphere.HostDetails{Domain: "lab.local"}}, older},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkCells(t, writeHostSheet(t, tc.host, "vHost"), tc.want)
		})
	}
}

func TestWriteRVToolsHostAdapterColumns(t *testing.T) {
	speed, yes := int32(1000), true
	wwpn := int64(0x2100001b32a1b2c3)
	details := &vsphere.HostDetails{Gateway: "192.168.150.1"}
	for _, tc := range []struct {
		name  string
		sheet string
		host  vsphere.Host
		want  map[string]string
	}{
		{"lab vNIC on a distributed switch", "vNIC", vsphere.Host{NICs: []vsphere.HostNIC{{Device: "vmnic1", Driver: "nvmxnet3", LinkSpeedMB: &speed,
			Duplex: &yes, MAC: "52:54:00:ee:d3:ae", Switch: "dvSwitch-Lab", Uplink: "Uplink 1", PCI: "0000:04:01.0"}}},
			map[string]string{"Host": "esx-1", "Network Device": "vmnic1", "Driver": "nvmxnet3", "Speed": "1000", "Duplex": "TRUE",
				"MAC": "52:54:00:ee:d3:ae", "Switch": "dvSwitch-Lab", "Uplink port": "Uplink 1", "PCI": "0000:04:01.0", "WakeOn": "FALSE"}},
		{"vNIC with its link down", "vNIC", vsphere.Host{NICs: []vsphere.HostNIC{{Device: "vmnic0", Switch: "vSwitch0"}}},
			map[string]string{"Speed": "", "Duplex": "", "Uplink port": ""}},
		{"Fibre Channel vHBA", "vHBA", vsphere.Host{HBAs: []vsphere.HostHBA{{Device: "vmhba2", Type: "HostFibreChannelHba", WWPN: &wwpn}}},
			map[string]string{"Device": "vmhba2", "WWN": "2100001B32A1B2C3"}},
		{"lab block vHBA", "vHBA", vsphere.Host{HBAs: []vsphere.HostHBA{{Device: "vmhba0", Type: "HostBlockHba"}}},
			map[string]string{"Device": "vmhba0", "WWN": ""}},
		{"lab vSC_VMK", "vSC_VMK", vsphere.Host{Details: details, VMKs: []vsphere.HostVMKernel{{Device: "vmk0", PortGroup: "Management Network",
			IP: "192.168.150.11", IPv6: []string{"fe80::5054:ff:fe86:5436/64"}}}},
			map[string]string{"Port Group": "Management Network", "Device": "vmk0", "IP 6 Address": "fe80::5054:ff:fe86:5436",
				"Gateway": "192.168.150.1", "IP 6 Gateway": ""}},
		{"vSC_VMK with several IPv6 addresses", "vSC_VMK", vsphere.Host{Details: &vsphere.HostDetails{IPv6Gateway: "2001:db8::1"},
			VMKs: []vsphere.HostVMKernel{{Device: "vmk1", IPv6: []string{"2001:db8::11/64", "fe80::1/64"}}}},
			map[string]string{"IP 6 Address": "2001:db8::11, fe80::1", "Gateway": "", "IP 6 Gateway": "2001:db8::1"}},
		{"vSC_VMK captured before schema 26", "vSC_VMK", vsphere.Host{VMKs: []vsphere.HostVMKernel{{Device: "vmk0"}}},
			map[string]string{"Gateway": "", "IP 6 Gateway": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkCells(t, writeHostSheet(t, tc.host, tc.sheet), tc.want)
		})
	}
}
