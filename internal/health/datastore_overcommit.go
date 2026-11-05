package health

import (
	"fmt"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/humanize"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// evaluateDatastoreOvercommitted flags a datastore whose used plus
// uncommitted space exceeds its capacity: its thin disks have been promised
// more than it holds, so their growth can fill it, stopping the VMs on it
// and refusing power-ons that need a swap file. An inaccessible datastore
// reports no uncommitted space and is left to datastore-inaccessible.
func evaluateDatastoreOvercommitted(in Input, emit func(Finding)) {
	for _, resource := range in.Data.Resources {
		if resource.Kind != "datastore" {
			continue
		}
		var d vsphere.Datastore
		if !assessment.DecodeResource(resource, &d) || !d.Overcommitted() {
			continue
		}
		provisioned, _ := d.ProvisionedBytes()
		pct, _ := d.ProvisionedPercent()
		obj := resourceObject(in.Data, resource, "datastore", d.Name, d.ID, d.Datacenter)
		emit(Finding{Rule: "datastore-overcommitted", Object: obj,
			Message: fmt.Sprintf("Datastore has provisioned %s%% of its capacity (%s of %s, %s free)", percent(pct), humanize.Bytes(provisioned), humanize.Bytes(d.CapacityBytes), humanize.Bytes(d.FreeBytes)),
			Evidence: []Evidence{
				{Field: "provisioned_percent", Observed: percent(pct), Expected: "100"},
				{Field: "uncommitted_bytes", Observed: humanize.Bytes(*d.UncommittedBytes)},
				{Field: "free_bytes", Observed: humanize.Bytes(d.FreeBytes)},
			}})
	}
}
