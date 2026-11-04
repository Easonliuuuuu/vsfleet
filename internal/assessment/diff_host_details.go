package assessment

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// hostDetailChanges compares the hosts' vHost details: configuration
// always, and the boot time, configuration status and quarantine state only
// with runtime fields. As with VM settings, a field is compared only when
// both captures recorded it.
func hostDetailChanges(before, after json.RawMessage, includeRuntime bool) []FieldChange {
	var a, b struct {
		Details *vsphere.HostDetails `json:"details"`
	}
	if json.Unmarshal(before, &a) != nil || json.Unmarshal(after, &b) != nil || a.Details == nil || b.Details == nil {
		return nil
	}
	configA, stateA := splitHostDetails(*a.Details)
	configB, stateB := splitHostDetails(*b.Details)
	out := recordedFieldChanges("details", joinedLists(configA), joinedLists(configB))
	if includeRuntime {
		out = append(out, recordedFieldChanges("details", stateA, stateB)...)
	}
	return out
}

// hostState is the part of a host's details that changes without anyone
// reconfiguring it.
type hostState struct {
	BootTime     *time.Time `json:"boot_time,omitempty"`
	ConfigStatus string     `json:"config_status,omitempty"`
	InQuarantine *bool      `json:"in_quarantine,omitempty"`
}

func splitHostDetails(d vsphere.HostDetails) (vsphere.HostDetails, hostState) {
	state := hostState{ConfigStatus: d.ConfigStatus, InQuarantine: d.InQuarantine}
	if d.BootTime != nil {
		boot := d.BootTime.UTC()
		state.BootTime = &boot
	}
	d.BootTime, d.ConfigStatus, d.InQuarantine = nil, "", nil
	return d, state
}

// joinedLists is v as a JSON object whose top-level lists, such as the DNS
// servers, are each one comma-joined value: a server added or removed then
// changes the list, where comparing element by element would see only the
// elements both captures have.
func joinedLists(v any) map[string]any {
	raw, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	for k, value := range m {
		if list, ok := value.([]any); ok {
			parts := make([]string, len(list))
			for i, item := range list {
				parts[i] = fmt.Sprint(item)
			}
			m[k] = strings.Join(parts, ", ")
		}
	}
	return m
}
