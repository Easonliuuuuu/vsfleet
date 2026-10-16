package rvimport

import (
	"reflect"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The "lab" cells are those of 192.168.150.11 in the real RVTools 4.8.1.4
// export of the nested lab, taken on a collector in America/Los_Angeles.
func TestVHostDetailsAreReadBack(t *testing.T) {
	path := writeFixtureWorkbook(t, nil)
	zone, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skipf("no time zone database: %v", err)
	}
	no, yes, zero := false, true, int32(0)
	boot := time.Date(2026, 10, 4, 13, 24, 29, 0, time.UTC)
	lab := map[string]string{
		"ESX Version": "VMware ESXi 8.0.3 build-24784735", "CPU Model": "13th Gen Intel(R) Core(TM) i5-13500", "# CPU": "4",
		"HT Available": "False", "HT Active": "False", "VMotion support": "True", "Storage VMotion support": "True",
		"Boot time": "2026/10/04 06:24:29", "DHCP": "False", "Domain": "lab.local", "NTPD running": "False", "Time Zone": "UTC",
		"Time Zone Name": "UTC", "GMT Offset": "0", "BIOS Vendor": "Ubuntu distribution of EDK II", "BIOS Version": "2025.11-3ubuntu7.2",
		"UUID": "b1170caf-31ff-4d8f-bf59-a480f8bb6844",
	}
	older := map[string]string{"ESX Version": "8.0.3"}
	for header := range lab {
		if header != "ESX Version" {
			older[header] = ""
		}
	}
	for _, tc := range []struct {
		name               string
		cells              map[string]string
		wantVersion, build string
		want               *vsphere.HostDetails
	}{
		{"lab", lab, "8.0.3", "24784735", &vsphere.HostDetails{
			ProductName: "VMware ESXi 8.0.3 build-24784735", CPUModel: "13th Gen Intel(R) Core(TM) i5-13500", CPUPackages: 4,
			HTAvailable: &no, HTActive: &no, VMotion: &yes, StorageVMotion: &yes, BootTime: &boot, DNSDHCP: &no, Domain: "lab.local",
			NTPDRunning: &no, TimeZone: "UTC", TimeZoneName: "UTC", GMTOffset: &zero, BIOSVendor: "Ubuntu distribution of EDK II",
			BIOSVersion: "2025.11-3ubuntu7.2", UUID: "b1170caf-31ff-4d8f-bf59-a480f8bb6844",
		}},
		{"older vsfleet workbook", older, "8.0.3", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openFixture(t, path)
			for header, value := range tc.cells {
				setColumnValue(t, f, sheetVHost, header, 2, value)
			}
			result, err := Parse(f, Options{Timezone: zone})
			if err != nil {
				t.Fatal(err)
			}
			h := findHost(result, "alpha", "esx-a1")
			if h == nil {
				t.Fatal("esx-a1 not imported")
			}
			if h.Version != tc.wantVersion || h.Build != tc.build {
				t.Errorf("version, build = %q, %q, want %q, %q", h.Version, h.Build, tc.wantVersion, tc.build)
			}
			if !reflect.DeepEqual(h.Details, tc.want) {
				t.Errorf("details = %+v\nwant %+v", h.Details, tc.want)
			}
		})
	}
}
