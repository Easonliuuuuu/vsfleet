package assessment

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// datastoreDetailChanges compares two datastores' vDatastore details: their
// configuration always, and what moves without anyone reconfiguring them
// (the configuration status, the VMs with files on them and the datastore
// cluster's free space) only with runtime fields. A field is compared only
// when both captures recorded it, and a list such as the mounting hosts is
// compared whole.
func datastoreDetailChanges(before, after json.RawMessage, includeRuntime bool) []FieldChange {
	var a, b struct {
		Details *vsphere.DatastoreDetails `json:"details"`
	}
	if json.Unmarshal(before, &a) != nil || json.Unmarshal(after, &b) != nil || a.Details == nil || b.Details == nil {
		return nil
	}
	configA, stateA := splitDatastoreDetails(*a.Details)
	configB, stateB := splitDatastoreDetails(*b.Details)
	out := recordedFieldChanges("details", joinedLists(configA), joinedLists(configB))
	if includeRuntime {
		out = append(out, recordedFieldChanges("details", joinedLists(stateA), joinedLists(stateB))...)
	}
	return out
}

// datastoreState is the part of a datastore's details that changes without
// anyone reconfiguring it.
type datastoreState struct {
	ConfigStatus         string   `json:"config_status,omitempty"`
	VMIDs                []string `json:"vm_ids"`
	ClusterCapacityBytes *int64   `json:"storage_cluster_capacity_bytes,omitempty"`
	ClusterFreeBytes     *int64   `json:"storage_cluster_free_bytes,omitempty"`
}

func splitDatastoreDetails(d vsphere.DatastoreDetails) (vsphere.DatastoreDetails, datastoreState) {
	state := datastoreState{ConfigStatus: d.ConfigStatus, VMIDs: d.VMIDs}
	if state.VMIDs == nil {
		state.VMIDs = []string{}
	}
	d.ConfigStatus, d.VMIDs = "", nil
	if d.Hosts == nil {
		d.Hosts = []string{}
	}
	if c := d.StorageCluster; c != nil {
		state.ClusterCapacityBytes, state.ClusterFreeBytes = c.CapacityBytes, c.FreeBytes
		d.StorageCluster = &vsphere.DatastoreCluster{Name: c.Name}
	}
	return d, state
}

// hostMultipathChanges compares the LUNs a host reaches and, for each LUN
// both captures recorded, its path policy and its paths: a path lost, added
// or gone dead is a change to how the host reaches its storage, so it is
// compared on every diff.
func hostMultipathChanges(before, after json.RawMessage) []FieldChange {
	var a, b struct {
		Multipaths []vsphere.HostMultipath `json:"multipaths"`
	}
	if json.Unmarshal(before, &a) != nil || json.Unmarshal(after, &b) != nil || len(a.Multipaths) == 0 || len(b.Multipaths) == 0 {
		return nil
	}
	lunsA, lunsB := multipathsByLUN(a.Multipaths), multipathsByLUN(b.Multipaths)
	var out []FieldChange
	if before, after := sortedNames(lunsA), sortedNames(lunsB); before != after {
		out = append(out, FieldChange{Field: "multipaths", Before: before, After: after})
	}
	names := make([]string, 0, len(lunsA))
	for name := range lunsA {
		if _, ok := lunsB[name]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		x, y := lunsA[name], lunsB[name]
		if x.Policy != y.Policy {
			out = append(out, FieldChange{Field: "multipaths[" + name + "].policy", Before: x.Policy, After: y.Policy})
		}
		if x.Device != nil && y.Device != nil {
			if before, after := pathsText(x.Device.Paths), pathsText(y.Device.Paths); before != after {
				out = append(out, FieldChange{Field: "multipaths[" + name + "].paths", Before: before, After: after})
			}
		}
	}
	return out
}

// multipathsByLUN keys a host's multipath entries by the LUN's canonical
// name, so a capture predating inventory schema 35, which recorded only the
// device path, keys a LUN the same as a newer one. An entry with neither is
// keyed by its display name.
func multipathsByLUN(entries []vsphere.HostMultipath) map[string]vsphere.HostMultipath {
	out := make(map[string]vsphere.HostMultipath, len(entries))
	for _, e := range entries {
		name := e.CanonicalName()
		if name == "" {
			name = e.LUN
		}
		out[name] = e
	}
	return out
}

func sortedNames(m map[string]vsphere.HostMultipath) string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// pathsText lists paths by adapter with their state, e.g. "vmhba1 active,
// vmhba2 dead": the adapter is what an RVTools workbook records, so a
// capture and its import compare equal.
func pathsText(paths []vsphere.HostMultipathPath) string {
	parts := make([]string, len(paths))
	for i, p := range paths {
		adapter := p.Adapter
		if adapter == "" {
			adapter = p.Name
		}
		parts[i] = adapter + " " + p.State
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
