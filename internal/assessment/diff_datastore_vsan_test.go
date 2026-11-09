package assessment

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestDatastoreVSANReserveChanges(t *testing.T) {
	read := func(ops, rebuild string, bytes int64) vsphere.Datastore {
		return vsphere.Datastore{ID: "datastore-1", Name: "vsan-a", Type: "vsan", VSANReserve: &vsphere.VSANCapacityReserve{OperationsState: ops, OperationsBytes: bytes, HostRebuildState: rebuild, HostRebuildBytes: bytes}}
	}
	cases := []struct {
		name          string
		before, after vsphere.Datastore
		want          []FieldChange
	}{
		{"operations reserve enforced", read("Reported", "Disabled", 1), read("Enforced", "Disabled", 1),
			[]FieldChange{{Field: "vsan_operations_reserve", Before: "Reported", After: "Enforced"}}},
		{"both reserves turned on after an upgrade", read("", "", 1), read("Enforced", "Enforced", 1),
			[]FieldChange{{Field: "vsan_operations_reserve", Before: "unsupported", After: "Enforced"}, {Field: "vsan_host_rebuild_reserve", Before: "unsupported", After: "Enforced"}}},
		{"only the sizes changed", read("Enforced", "Enforced", 1), read("Enforced", "Enforced", 2), nil},
		{"a capture that did not read reserves", vsphere.Datastore{ID: "datastore-1", Name: "vsan-a", Type: "vsan"}, read("Enforced", "Enforced", 1), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := json.Marshal(tc.before)
			after, _ := json.Marshal(tc.after)
			var got []FieldChange
			for _, f := range changedResourceFields("datastore", before, after, true) {
				if f.Field == "vsan_operations_reserve" || f.Field == "vsan_host_rebuild_reserve" {
					got = append(got, f)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("changes = %+v, want %+v", got, tc.want)
			}
		})
	}
}
