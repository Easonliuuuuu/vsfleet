package assessment

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestHostDetailChanges(t *testing.T) {
	one, zero, no, yes := int64(1), int64(0), false, true
	boot := time.Date(2026, 10, 4, 13, 24, 29, 0, time.UTC)
	reboot := boot.Add(48 * time.Hour)
	lab := vsphere.HostDetails{ConfigStatus: "yellow", InQuarantine: &no, ATSHeartbeat: &one, HostPowerPolicy: "dynamic",
		SearchDomains: []string{"lab.local"}, Gateway: "192.168.150.1", BootTime: &boot}
	payload := func(d *vsphere.HostDetails) json.RawMessage {
		b, err := json.Marshal(vsphere.Host{ID: "host-12", Name: "192.168.150.11", Details: d})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	edit := func(f func(*vsphere.HostDetails)) *vsphere.HostDetails {
		d := lab
		f(&d)
		return &d
	}
	for _, tc := range []struct {
		name    string
		after   *vsphere.HostDetails
		runtime bool
		want    []string
	}{
		{"unchanged", &lab, true, nil},
		{"configuration change", edit(func(d *vsphere.HostDetails) {
			d.ATSHeartbeat, d.HostPowerPolicy, d.SearchDomains = &zero, "static", []string{"lab.local", "corp.local"}
		}), false, []string{"details.ats_heartbeat", "details.host_power_policy", "details.search_domains"}},
		{"state change without runtime fields", edit(func(d *vsphere.HostDetails) {
			d.ConfigStatus, d.InQuarantine, d.BootTime = "green", &yes, &reboot
		}), false, nil},
		{"state change with runtime fields", edit(func(d *vsphere.HostDetails) {
			d.ConfigStatus, d.InQuarantine, d.BootTime = "green", &yes, &reboot
		}), true, []string{"details.boot_time", "details.config_status", "details.in_quarantine"}},
		// A capture predating inventory schema 30, or a workbook without the
		// columns, recorded none of them.
		{"field not recorded", edit(func(d *vsphere.HostDetails) { d.ATSHeartbeat, d.Gateway = nil, "" }), true, nil},
		{"details not read", nil, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, c := range changedResourceFields("host", payload(&lab), payload(tc.after), tc.runtime) {
				got = append(got, c.Field)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("changed fields = %v, want %v", got, tc.want)
			}
		})
	}
}
