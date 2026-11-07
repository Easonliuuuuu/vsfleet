package tui

import (
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestDatastoreDetailShowsAlarms(t *testing.T) {
	usage := func(status string) vsphere.Alarm {
		return vsphere.Alarm{Name: "Datastore usage on disk", Status: status, Entity: "san-01", EntityType: "Datastore"}
	}
	for _, tc := range []struct {
		name   string
		read   bool
		alarms []vsphere.Alarm
		want   string
		mark   rowStatus
	}{
		{"critical", true, []vsphere.Alarm{usage("red"), usage("yellow")}, "1 critical · 1 warning · Datastore usage on disk", statusBad},
		{"warning", true, []vsphere.Alarm{usage("yellow")}, "1 warning · Datastore usage on disk", statusWarn},
		{"none", true, nil, "none", statusNone},
		{"not read", false, nil, "-", statusNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := vsphere.Datastore{Name: "san-01", Accessible: true, CapacityBytes: 100 << 30, FreeBytes: 40 << 30, AlarmsRead: tc.read, Alarms: tc.alarms}
			r := datastoreRow(d, nil, false)
			if got, _ := fieldValue(r.detail, "Alarms"); got != tc.want {
				t.Errorf("Alarms = %q, want %q", got, tc.want)
			}
			if got := r.marks["Alarms"].status; got != tc.mark {
				t.Errorf("Alarms mark = %v, want %v", got, tc.mark)
			}
		})
	}
}
