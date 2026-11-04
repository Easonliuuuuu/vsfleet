package assessment

import (
	"sort"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// recordedFieldChanges compares two optional structs field by field under
// prefix. A field is compared only when both sides recorded it: a capture
// predating inventory schema 29, a vCenter too old for the property, or a
// workbook without the column leaves it unknown rather than changed.
func recordedFieldChanges(prefix string, a, b any) []FieldChange {
	before, after := flattenJSON(a), flattenJSON(b)
	var out []FieldChange
	for path, old := range before {
		if now, ok := after[path]; ok && now != old {
			out = append(out, FieldChange{Field: prefix + "." + path, Before: old, After: now})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

func vmSettingsChanges(a, b *vsphere.VMSettings) []FieldChange {
	if a == nil || b == nil {
		return nil
	}
	return recordedFieldChanges("settings", a, b)
}

func vmStateChanges(a, b *vsphere.VMState) []FieldChange {
	if a == nil || b == nil {
		return nil
	}
	return recordedFieldChanges("state", a, b)
}
