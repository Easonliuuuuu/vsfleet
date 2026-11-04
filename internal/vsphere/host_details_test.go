package vsphere

import (
	"reflect"
	"testing"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// The simulator reports no time service, so the time zone, NTP and ntpd
// mapping is covered here.
func TestMapHostDetailsTimeServices(t *testing.T) {
	running, offset := true, int32(-25200)
	for _, tc := range []struct {
		name   string
		config *types.HostConfigInfo
		want   *HostDetails
	}{
		{"configured", &types.HostConfigInfo{
			DateTimeInfo: &types.HostDateTimeInfo{
				TimeZone:  types.HostDateTimeSystemTimeZone{Key: "America/Los_Angeles", Name: "Pacific", GmtOffset: offset},
				NtpConfig: &types.HostNtpConfig{Server: []string{"ntp1.example", "ntp2.example"}},
			},
			Service: &types.HostServiceInfo{Service: []types.HostService{{Key: "TSM-SSH"}, {Key: "ntpd", Running: true}}},
		}, &HostDetails{TimeZone: "America/Los_Angeles", TimeZoneName: "Pacific", GMTOffset: &offset,
			NTPServers: []string{"ntp1.example", "ntp2.example"}, NTPDRunning: &running}},
		{"not read", nil, &HostDetails{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mapHostDetails(&mo.HostSystem{Config: tc.config})
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("details = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// The simulator reports none of vHost's power, tiering, quarantine, domain,
// gateway or identifier fields; the "lab" case is what the nested lab's
// vCenter 8.0.3 returns for 192.168.150.12.
func TestMapHostDetailsConfiguration(t *testing.T) {
	no := false
	lab := &mo.HostSystem{
		ManagedEntity: mo.ManagedEntity{ConfigStatus: types.ManagedEntityStatusYellow},
		Runtime:       types.HostRuntimeInfo{InQuarantineMode: &no},
		Summary: types.HostListSummary{Hardware: &types.HostHardwareSummary{OtherIdentifyingInfo: []types.HostSystemIdentificationInfo{
			{IdentifierType: &types.ElementDescription{Key: "AssetTag"}},
			{IdentifierType: &types.ElementDescription{Key: "ServiceTag"}},
		}}},
		Hardware: &types.HostHardwareInfo{MemoryTieringType: "noTiering",
			CpuPowerManagementInfo: &types.HostCpuPowerManagementInfo{CurrentPolicy: "Balanced"}},
		Config: &types.HostConfigInfo{
			PowerSystemInfo: &types.PowerSystemInfo{CurrentPolicy: types.HostPowerPolicy{Key: 2, ShortName: "dynamic"}},
			Network: &types.HostNetworkInfo{
				DnsConfig:     &types.HostDnsConfig{DomainName: "lab.local"},
				IpRouteConfig: &types.HostIpRouteConfig{DefaultGateway: "192.168.150.1"},
			},
		},
	}
	dell := &mo.HostSystem{
		Summary: types.HostListSummary{Hardware: &types.HostHardwareSummary{OtherIdentifyingInfo: []types.HostSystemIdentificationInfo{
			{IdentifierValue: "7XK2Q3", IdentifierType: &types.ElementDescription{Key: "ServiceTag"}},
			{IdentifierValue: "Dell System", IdentifierType: &types.ElementDescription{Key: "OemSpecificString"}},
			{IdentifierValue: "5[0000]", IdentifierType: &types.ElementDescription{Key: "OemSpecificString"}},
		}}},
		Config: &types.HostConfigInfo{
			DomainList: []string{"CORP"},
			Network: &types.HostNetworkInfo{
				DnsConfig:     &types.HostDnsConfig{SearchDomain: []string{"lab.local", "corp.local"}},
				IpRouteConfig: &types.HostIpRouteConfig{IpV6DefaultGateway: "2001:db8::1"},
			},
		},
	}
	for _, tc := range []struct {
		name string
		host *mo.HostSystem
		want *HostDetails
	}{
		{"lab", lab, &HostDetails{ConfigStatus: "yellow", InQuarantine: &no, MemoryTiering: "noTiering", CPUPowerPolicy: "Balanced",
			HostPowerPolicy: "dynamic", Domain: "lab.local", DNSDHCP: &no, Gateway: "192.168.150.1"}},
		{"identifiers and lists", dell, &HostDetails{ServiceTag: "7XK2Q3", OEMString: "Dell System, 5[0000]", DomainList: []string{"CORP"},
			SearchDomains: []string{"lab.local", "corp.local"}, DNSDHCP: &no, IPv6Gateway: "2001:db8::1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mapHostDetails(tc.host); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("details = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// RetrieveProperties returns each setting under its key selector, and a
// setting the host lacks in the missing set.
func TestATSFromContent(t *testing.T) {
	lab := types.ManagedObjectReference{Type: "OptionManager", Value: "EsxHostAdvSettings-17"}
	old := types.ManagedObjectReference{Type: "OptionManager", Value: "EsxHostAdvSettings-20"}
	got := atsFromContent([]types.ObjectContent{
		{Obj: lab, PropSet: []types.DynamicProperty{
			// vCenter 8.0.3 returns the setting by value.
			{Name: `setting["VMFS3.HardwareAcceleratedLocking"]`, Val: types.OptionValue{Key: "VMFS3.HardwareAcceleratedLocking", Value: int64(1)}},
			{Name: `setting["VMFS3.UseATSForHBOnVMFS5"]`, Val: &types.OptionValue{Key: "VMFS3.UseATSForHBOnVMFS5", Value: int32(0)}},
		}},
		{Obj: old, PropSet: []types.DynamicProperty{
			{Name: `setting["VMFS3.HardwareAcceleratedLocking"]`, Val: &types.OptionValue{Key: "VMFS3.HardwareAcceleratedLocking", Value: int64(1)}},
		}, MissingSet: []types.MissingProperty{{Path: `setting["VMFS3.UseATSForHBOnVMFS5"]`}}},
	})
	value := func(p *int64) any {
		if p == nil {
			return nil
		}
		return *p
	}
	for _, tc := range []struct {
		ref                types.ManagedObjectReference
		heartbeat, locking any
	}{{lab, int64(0), int64(1)}, {old, nil, int64(1)}} {
		v := got[tc.ref]
		if value(v[0]) != tc.heartbeat || value(v[1]) != tc.locking {
			t.Errorf("%s: heartbeat, locking = %v, %v, want %v, %v", tc.ref.Value, value(v[0]), value(v[1]), tc.heartbeat, tc.locking)
		}
	}
}

func TestHostDetailPropsForAPI(t *testing.T) {
	for _, tc := range []struct {
		version string
		tiering bool
	}{{"7.0.2.0", false}, {"7.0.3.0", true}, {"8.0.3.0", true}} {
		got := false
		for _, p := range hostDetailPropsForAPI(tc.version) {
			got = got || p == "hardware.memoryTieringType"
		}
		if got != tc.tiering {
			t.Errorf("API %s reads memoryTieringType = %v, want %v", tc.version, got, tc.tiering)
		}
	}
}
