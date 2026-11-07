package health

import (
	"fmt"
	"sort"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// evaluateStorageDRSFull reports a datastore cluster with Storage DRS on
// whose every member is above its space threshold or inaccessible: Storage
// DRS avoids such a datastore during initial placement, so the cluster has
// nowhere left it prefers to put a new disk.
func evaluateStorageDRSFull(in Input, emit func(Finding)) {
	type member struct {
		resource assessment.ResourceObservation
		ds       vsphere.Datastore
	}
	type pod struct {
		cluster vsphere.DatastoreCluster
		members []member
	}
	pods := map[string]*pod{}
	for _, resource := range in.Data.Resources {
		if resource.Kind != "datastore" {
			continue
		}
		var d vsphere.Datastore
		if !assessment.DecodeResource(resource, &d) || d.Details == nil || d.Details.StorageCluster == nil {
			continue
		}
		cl := d.Details.StorageCluster
		if cl.StorageDRS == nil || !cl.StorageDRS.Enabled {
			continue
		}
		key := resource.VCenterID + "\x00" + cl.ID
		if pods[key] == nil {
			pods[key] = &pod{cluster: *cl}
		}
		pods[key].members = append(pods[key].members, member{resource, d})
	}
	keys := make([]string, 0, len(pods))
	for key := range pods {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		p := pods[key]
		var threshold string
		above := 0
		for _, m := range p.members {
			over, t := p.cluster.StorageDRS.AboveThreshold(m.ds.CapacityBytes, m.ds.FreeBytes)
			threshold = t
			if over || !m.ds.Accessible {
				above++
			}
		}
		if above < len(p.members) {
			continue
		}
		first := p.members[0]
		obj := resourceObject(in.Data, first.resource, "datastorecluster", p.cluster.Name, p.cluster.ID, first.ds.Datacenter)
		emit(Finding{Rule: "storage-drs-space-threshold", Object: obj,
			Message: fmt.Sprintf("Every datastore in the cluster is past Storage DRS's %s space threshold (%d of %d)", threshold, above, len(p.members)),
			Evidence: []Evidence{
				{Field: "datastores_past_threshold", Observed: fmt.Sprintf("%d of %d", above, len(p.members)), Expected: fmt.Sprintf("fewer than %d", len(p.members))},
				{Field: "space_threshold", Observed: threshold},
			}})
	}
}
