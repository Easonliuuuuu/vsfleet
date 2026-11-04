package assessment

import (
	"fmt"
	"strconv"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// vmFileChanges reports a moved configuration, log, snapshot or suspend
// location. A side that did not record its files, a capture predating
// inventory schema 28 or an import of a workbook without the columns, leaves
// them unknown rather than changed.
func vmFileChanges(a, b *vsphere.VMFiles) []FieldChange {
	if a == nil || b == nil {
		return nil
	}
	var out []FieldChange
	for _, f := range []struct{ field, before, after string }{
		{"files.vm_path", a.VMPath, b.VMPath},
		{"files.log_directory", a.LogDirectory, b.LogDirectory},
		{"files.snapshot_directory", a.SnapshotDirectory, b.SnapshotDirectory},
		{"files.suspend_directory", a.SuspendDirectory, b.SuspendDirectory},
	} {
		if before, after := knownBoth(f.before, f.after), knownBoth(f.after, f.before); before != after {
			out = append(out, FieldChange{Field: f.field, Before: before, After: after})
		}
	}
	return out
}

// vmStorageChanges reports the volatile storage fields added in inventory
// schema 28, each only when both sides recorded it.
func vmStorageChanges(a, b vsphere.VM) []FieldChange {
	var out []FieldChange
	gib := func(field string, before, after *int64) {
		if before == nil || after == nil {
			return
		}
		if bv, av := fmt.Sprintf("%.3f", float64(*before)/(1<<30)), fmt.Sprintf("%.3f", float64(*after)/(1<<30)); bv != av {
			out = append(out, FieldChange{Field: field, Before: bv, After: av})
		}
	}
	gib("provisioned_gb", a.ProvisionedBytes, b.ProvisionedBytes)
	gib("unshared_gb", a.UnsharedBytes, b.UnsharedBytes)
	if a.ConsolidationNeeded != nil && b.ConsolidationNeeded != nil && *a.ConsolidationNeeded != *b.ConsolidationNeeded {
		out = append(out, FieldChange{Field: "consolidation_needed", Before: strconv.FormatBool(*a.ConsolidationNeeded), After: strconv.FormatBool(*b.ConsolidationNeeded)})
	}
	return out
}
