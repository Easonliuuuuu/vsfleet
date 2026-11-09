package health

import (
	"fmt"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/humanize"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// evaluateDatastoreSpaceLow reports a datastore below the warning level as a
// warning, and one below either critical floor, a percentage or a number of
// bytes, as critical. The defaults line up with vCenter: its "Datastore usage
// on disk" alarm turns red at 85% used, which is the 15% free warning level.
func evaluateDatastoreSpaceLow(in Input, thresholds Thresholds, emit func(Finding)) {
	if thresholds.DatastoreWarnFreePct <= 0 && thresholds.DatastoreFreePct <= 0 && thresholds.DatastoreFreeBytes <= 0 {
		return
	}
	for _, resource := range in.Data.Resources {
		if resource.Kind != "datastore" {
			continue
		}
		var datastore vsphere.Datastore
		if !assessment.DecodeResource(resource, &datastore) {
			continue
		}
		// A vSAN datastore's enforced capacity reserves are free space no
		// new disk can use, so they count as used here.
		freeBytes := datastore.ProvisionableFreeBytes()
		reserved := datastore.FreeBytes - freeBytes
		free, ok := freePct(datastore.CapacityBytes, freeBytes)
		percentBreached := thresholds.DatastoreFreePct > 0 && ok && free < thresholds.DatastoreFreePct
		bytesBreached := thresholds.DatastoreFreeBytes > 0 && float64(freeBytes) < thresholds.DatastoreFreeBytes
		warned := thresholds.DatastoreWarnFreePct > 0 && ok && free < thresholds.DatastoreWarnFreePct
		if !percentBreached && !bytesBreached && !warned {
			continue
		}
		severity, floor := SeverityWarning, thresholds.DatastoreWarnFreePct
		if percentBreached || bytesBreached {
			severity, floor = SeverityCritical, thresholds.DatastoreFreePct
		}
		evidence := make([]Evidence, 0, 3)
		if warned || percentBreached {
			evidence = append(evidence, Evidence{Field: "free_percent", Observed: percent(free), Expected: percent(floor)})
		}
		if bytesBreached {
			evidence = append(evidence, Evidence{Field: "free_bytes", Observed: humanize.Bytes(freeBytes), Expected: humanize.Bytes(int64(thresholds.DatastoreFreeBytes))})
		}
		message := fmt.Sprintf("Datastore has %s%% free space (%s of %s)", percent(free), humanize.Bytes(freeBytes), humanize.Bytes(datastore.CapacityBytes))
		if bytesBreached && len(evidence) == 1 {
			message = fmt.Sprintf("Datastore has %s free space, below the %s floor", humanize.Bytes(freeBytes), humanize.Bytes(int64(thresholds.DatastoreFreeBytes)))
		} else if bytesBreached {
			message += fmt.Sprintf(" and below the %s floor", humanize.Bytes(int64(thresholds.DatastoreFreeBytes)))
		}
		if reserved > 0 {
			message += fmt.Sprintf(", after %s held by vSAN's enforced capacity reserves", humanize.Bytes(reserved))
			evidence = append(evidence, Evidence{Field: "vsan_reserved_bytes", Observed: humanize.Bytes(reserved)})
		}
		obj := resourceObject(in.Data, resource, "datastore", datastore.Name, datastore.ID, datastore.Datacenter)
		emit(Finding{Rule: "datastore-space-low", Severity: severity, Object: obj, Message: message, Evidence: evidence})
	}
}
