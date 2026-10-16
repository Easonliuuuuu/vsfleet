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
