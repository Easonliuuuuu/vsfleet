package assessment

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// datastoreAlarmChanges compares the alarms triggered on a datastore, by name
// and colour, so a diff shows when one fired, changed colour or cleared. It
// is compared on every diff, like a dead storage path: an alarm is what an
// admin acts on. Only captures that both read alarms are compared, since an
// older capture or an import has none to compare.
func datastoreAlarmChanges(before, after json.RawMessage) []FieldChange {
	var a, b vsphere.Datastore
	if json.Unmarshal(before, &a) != nil || json.Unmarshal(after, &b) != nil || !a.AlarmsRead || !b.AlarmsRead {
		return nil
	}
	if x, y := alarmsText(a.Alarms), alarmsText(b.Alarms); x != y {
		return []FieldChange{{Field: "alarms", Before: x, After: y}}
	}
	return nil
}

// alarmsText lists alarms as "name (colour)", sorted, or "none".
func alarmsText(alarms []vsphere.Alarm) string {
	if len(alarms) == 0 {
		return "none"
	}
	parts := make([]string, len(alarms))
	for i, a := range alarms {
		parts[i] = a.Name + " (" + a.Status + ")"
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
