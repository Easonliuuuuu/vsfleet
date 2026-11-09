package assessment

import (
	"encoding/json"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// datastoreVSANReserveChanges compares the state of a vSAN datastore's
// capacity reserves, such as the operations reserve going from "Reported"
// to "Enforced". The reserves' sizes follow the cluster's capacity and are
// not compared. Only captures that both read the reserves are compared.
func datastoreVSANReserveChanges(before, after json.RawMessage) []FieldChange {
	var a, b vsphere.Datastore
	if json.Unmarshal(before, &a) != nil || json.Unmarshal(after, &b) != nil || a.VSANReserve == nil || b.VSANReserve == nil {
		return nil
	}
	var out []FieldChange
	if x, y := reserveState(a.VSANReserve.OperationsState), reserveState(b.VSANReserve.OperationsState); x != y {
		out = append(out, FieldChange{Field: "vsan_operations_reserve", Before: x, After: y})
	}
	if x, y := reserveState(a.VSANReserve.HostRebuildState), reserveState(b.VSANReserve.HostRebuildState); x != y {
		out = append(out, FieldChange{Field: "vsan_host_rebuild_reserve", Before: x, After: y})
	}
	return out
}

func reserveState(s string) string {
	if s == "" {
		return "unsupported"
	}
	return s
}
