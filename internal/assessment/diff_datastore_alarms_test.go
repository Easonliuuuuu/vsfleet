package assessment

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestDatastoreAlarmChanges(t *testing.T) {
	usage := func(status string) vsphere.Alarm {
		return vsphere.Alarm{Name: "Datastore usage on disk", Status: status, Entity: "san-01", EntityType: "Datastore"}
	}
	read := func(alarms ...vsphere.Alarm) vsphere.Datastore {
		return vsphere.Datastore{ID: "datastore-1", Name: "san-01", AlarmsRead: true, Alarms: alarms}
	}
	cases := []struct {
		name          string
		before, after vsphere.Datastore
		want          []FieldChange
	}{
		{"an alarm fired", read(), read(usage("yellow")),
			[]FieldChange{{Field: "alarms", Before: "none", After: "Datastore usage on disk (yellow)"}}},
		{"an alarm turned red", read(usage("yellow")), read(usage("red")),
			[]FieldChange{{Field: "alarms", Before: "Datastore usage on disk (yellow)", After: "Datastore usage on disk (red)"}}},
		{"an alarm cleared", read(usage("red")), read(),
			[]FieldChange{{Field: "alarms", Before: "Datastore usage on disk (red)", After: "none"}}},
		{"acknowledged only", read(usage("red")), func() vsphere.Datastore { a := usage("red"); a.Acknowledged = true; return read(a) }(), nil},
		{"a capture that did not read alarms", vsphere.Datastore{ID: "datastore-1", Name: "san-01"}, read(usage("red")), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := json.Marshal(tc.before)
			after, _ := json.Marshal(tc.after)
			var got []FieldChange
			for _, f := range changedResourceFields("datastore", before, after, false) {
				if f.Field == "alarms" {
					got = append(got, f)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("changes = %+v, want %+v", got, tc.want)
			}
		})
	}
}
