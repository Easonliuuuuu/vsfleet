package rvimport

import (
	"reflect"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The lab hosts' boot time, BIOS date and certificate, as vCenter reports
// them: the boot time has a fraction of a second.
var (
	labBoot        = time.Date(2026, 11, 2, 12, 24, 40, 840856000, time.UTC)
	labBIOS        = time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	labCertificate = vsphere.HostCertificate{Issuer: "CN=CA", Subject: "CN=192.168.150.11",
		NotBefore: time.Date(2026, 9, 26, 16, 9, 31, 0, time.UTC), NotAfter: time.Date(2031, 9, 26, 16, 9, 31, 0, time.UTC)}
)

// hostConfigFixture writes a workbook whose esx-a1 carries every column
// inventory schema 30 added, with the nested lab's values where it has them.
func hostConfigFixture(t *testing.T) (string, *vsphere.HostDetails) {
	t.Helper()
	no, one := false, int64(1)
	speed, yes := int32(1000), true
	wwpn := int64(-0x2000000000000001) // a WWN whose top bit is set
	details := &vsphere.HostDetails{ConfigStatus: "yellow", InQuarantine: &no, MemoryTiering: "noTiering", ATSHeartbeat: &one,
		ATSLocking: &one, CPUPowerPolicy: "Balanced", CPUPowerSupport: "ACPI P-states", HostPowerPolicy: "dynamic",
		DomainList: []string{"CORP", "LAB"}, SearchDomains: []string{"lab.local", "corp.local"}, ServiceTag: "7XK2Q3",
		OEMString: "Dell System, 5[0000]", Gateway: "192.168.150.1", IPv6Gateway: "2001:db8::1"}
	path := writeFixtureWorkbook(t, func(data *assessment.ExportData) {
		data.Run.InventorySchemaVersion = assessment.CurrentInventorySchemaVersion
		editHost(t, data, "host-a1", func(h *vsphere.Host) {
			withDates := *details
			withDates.BootTime, withDates.BIOSDate = &labBoot, &labBIOS
			withDates.Certificate = &labCertificate
			h.Details = &withDates
			h.NICs = []vsphere.HostNIC{{Device: "vmnic1", Driver: "nvmxnet3", LinkSpeedMB: &speed, Duplex: &yes, MAC: "52:54:00:ee:d3:ae",
				Switch: "dvSwitch-Lab", Uplink: "Uplink 1", PCI: "0000:04:01.0"}}
			h.HBAs = []vsphere.HostHBA{{Device: "vmhba2", Type: "HostFibreChannelHba", Status: "online", WWPN: &wwpn}}
			h.VMKs = []vsphere.HostVMKernel{{Device: "vmk0", PortGroup: "Management Network", IP: "192.168.150.11",
				IPv6: []string{"fe80::5054:ff:fe86:5436/64"}}}
		})
	})
	return path, details
}

func TestHostConfigurationColumnsAreReadBack(t *testing.T) {
	path, details := hostConfigFixture(t)
	f := openFixture(t, path)
	result, err := Parse(f, Options{})
	if err != nil {
		t.Fatal(err)
	}
	h := findHost(result, "alpha", "esx-a1")
	if h == nil || h.Details == nil {
		t.Fatal("esx-a1 or its details not imported")
	}
	got := *h.Details
	got = vsphere.HostDetails{ConfigStatus: got.ConfigStatus, InQuarantine: got.InQuarantine, MemoryTiering: got.MemoryTiering,
		ATSHeartbeat: got.ATSHeartbeat, ATSLocking: got.ATSLocking, CPUPowerPolicy: got.CPUPowerPolicy, CPUPowerSupport: got.CPUPowerSupport,
		HostPowerPolicy: got.HostPowerPolicy, DomainList: got.DomainList, SearchDomains: got.SearchDomains, ServiceTag: got.ServiceTag,
		OEMString: got.OEMString, Gateway: got.Gateway, IPv6Gateway: got.IPv6Gateway}
	if !reflect.DeepEqual(&got, details) {
		t.Errorf("details = %+v\nwant %+v", got, *details)
	}
	// vHost's dates are date cells, read back to the second.
	if h.Details.BootTime == nil || !h.Details.BootTime.Equal(labBoot.Round(time.Second)) ||
		h.Details.BIOSDate == nil || !h.Details.BIOSDate.Equal(labBIOS) ||
		h.Details.Certificate == nil || *h.Details.Certificate != labCertificate {
		t.Errorf("boot time, BIOS date, certificate = %v, %v, %+v", h.Details.BootTime, h.Details.BIOSDate, h.Details.Certificate)
	}
	if len(h.NICs) != 1 || h.NICs[0].Device != "vmnic1" || h.NICs[0].MAC != "52:54:00:ee:d3:ae" || h.NICs[0].Uplink != "Uplink 1" ||
		h.NICs[0].LinkSpeedMB == nil || *h.NICs[0].LinkSpeedMB != 1000 {
		t.Errorf("NICs = %+v", h.NICs)
	}
	if len(h.HBAs) != 1 || h.HBAs[0].WWPN == nil || *h.HBAs[0].WWPN != -0x2000000000000001 {
		t.Errorf("HBAs = %+v, want the WWPN back exactly", h.HBAs)
	}
	if len(h.VMKs) != 1 || !reflect.DeepEqual(h.VMKs[0].IPv6, []string{"fe80::5054:ff:fe86:5436"}) {
		t.Errorf("VMKs = %+v", h.VMKs)
	}
}

// vsfleet named its vNIC columns Device, Mac Address, Link speed Mb and Wake
// on LAN before inventory schema 30.
func TestOlderVNICColumnNamesAreRead(t *testing.T) {
	path, _ := hostConfigFixture(t)
	f := openFixture(t, path)
	for current, older := range map[string]string{"Network Device": "Device", "MAC": "Mac Address", "Speed": "Link speed Mb", "WakeOn": "Wake on LAN"} {
		setColumnValue(t, f, sheetVNIC, current, 1, older)
	}
	result, err := Parse(f, Options{})
	if err != nil {
		t.Fatal(err)
	}
	h := findHost(result, "alpha", "esx-a1")
	if h == nil || len(h.NICs) != 1 {
		t.Fatalf("esx-a1 NICs not imported: %+v", h)
	}
	if n := h.NICs[0]; n.Device != "vmnic1" || n.MAC != "52:54:00:ee:d3:ae" || n.LinkSpeedMB == nil || *n.LinkSpeedMB != 1000 {
		t.Errorf("NIC = %+v", n)
	}
}

func TestParseHexWWN(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want *int64
	}{
		{"2100001B32A1B2C3", ptr(int64(0x2100001b32a1b2c3))},
		{"21:00:00:1b:32:a1:b2:c3", ptr(int64(0x2100001b32a1b2c3))},
		{"DFFFFFFFFFFFFFFF", ptr(int64(-0x2000000000000001))},
		{"FFFFFFFFFFFFFFFF", ptr(int64(-1))},
		{"1B", ptr(int64(0x1b))},
		{"", nil},
		{"12345678901234567", nil},
		{"-1", nil},
		{"12345678-1234567", nil},
		{"zz", nil},
	} {
		got := parseHexWWN(tc.in)
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Errorf("parseHexWWN(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func ptr[T any](v T) *T { return &v }
