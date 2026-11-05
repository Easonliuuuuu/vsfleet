package assessment

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// hostNetworkChanges compares, for each standard switch and port group both
// captures recorded, its configuration: security, teaming, shaping, offload,
// MTU and uplinks. A switch's free ports change without anyone reconfiguring
// it, so they are compared only with runtime fields. As with VM settings, a
// field is compared only when both captures recorded it, so a capture from
// before a field was collected never reports it as changed.
func hostNetworkChanges(before, after json.RawMessage, includeRuntime bool) []FieldChange {
	var a, b struct {
		VSwitches  []vsphere.HostVSwitch   `json:"vswitches"`
		PortGroups []vsphere.HostPortGroup `json:"port_groups"`
	}
	if json.Unmarshal(before, &a) != nil || json.Unmarshal(after, &b) != nil {
		return nil
	}
	out := namedChanges("vswitches", byName(a.VSwitches, hostVSwitchName), byName(b.VSwitches, hostVSwitchName),
		func(sw vsphere.HostVSwitch) any {
			sw.Key = ""
			if !includeRuntime {
				sw.FreePorts = 0
			}
			return sw
		})
	return append(out, namedChanges("port_groups", byName(a.PortGroups, hostPortGroupName), byName(b.PortGroups, hostPortGroupName),
		func(p vsphere.HostPortGroup) any {
			p.Key = ""
			return p
		})...)
}

// dvSwitchChanges compares a distributed switch's settings and, for each
// port group both captures recorded, the port group's. Which port groups the
// switch has is compared whole. The number of VMs on the switch moves with
// the VMs, so it is compared only with runtime fields.
func dvSwitchChanges(before, after json.RawMessage, includeRuntime bool) []FieldChange {
	var a, b vsphere.DVSwitch
	if json.Unmarshal(before, &a) != nil || json.Unmarshal(after, &b) != nil {
		return nil
	}
	settings := func(sw vsphere.DVSwitch) map[string]any {
		s := dvSwitchSettings{ProductName: sw.ProductName, Created: sw.Created, DVSTrafficShaping: sw.DVSTrafficShaping}
		// A switch with no link aggregation groups records none, so whether
		// the groups were read at all is told by VMCount, read with them.
		if sw.VMCount != nil {
			lacp := lacpText(sw.LACPGroups)
			s.LACPGroups = &lacp
		}
		if includeRuntime {
			s.VMCount = sw.VMCount
		}
		return joinedLists(s)
	}
	out := recordedFieldChanges("settings", settings(a), settings(b))
	groupsA, groupsB := byName(a.PortGroups, dvPortGroupName), byName(b.PortGroups, dvPortGroupName)
	if before, after := sortedKeyList(groupsA), sortedKeyList(groupsB); before != after {
		out = append(out, FieldChange{Field: "port_groups", Before: before, After: after})
	}
	return append(out, namedChanges("port_groups", groupsA, groupsB, func(p vsphere.DVPortGroup) any { return p })...)
}

// dvSwitchSettings are the distributed switch settings dvSwitchChanges
// compares beyond the fields every resource diff compares.
type dvSwitchSettings struct {
	ProductName string     `json:"product_name,omitempty"`
	Created     *time.Time `json:"created,omitempty"`
	VMCount     *int       `json:"vm_count,omitempty"`
	LACPGroups  *string    `json:"lacp_groups,omitempty"`
	vsphere.DVSTrafficShaping
}

func lacpText(groups []vsphere.DVSLACPGroup) string {
	parts := make([]string, len(groups))
	for i, g := range groups {
		parts[i] = g.Name + " (" + g.Mode + ", " + g.LoadBalanceAlgorithm + ")"
	}
	return strings.Join(parts, "; ")
}

func hostVSwitchName(sw vsphere.HostVSwitch) string    { return sw.Name }
func hostPortGroupName(p vsphere.HostPortGroup) string { return p.Name }
func dvPortGroupName(p vsphere.DVPortGroup) string     { return p.Name }

func byName[T any](items []T, name func(T) string) map[string]T {
	out := make(map[string]T, len(items))
	for _, item := range items {
		out[name(item)] = item
	}
	return out
}

func sortedKeyList[T any](m map[string]T) string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// namedChanges compares the items both sides have under the same name,
// field by field, as "<prefix>[<name>].<field>".
func namedChanges[T any](prefix string, a, b map[string]T, comparable func(T) any) []FieldChange {
	names := make([]string, 0, len(a))
	for name := range a {
		if _, ok := b[name]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var out []FieldChange
	for _, name := range names {
		out = append(out, recordedFieldChanges(prefix+"["+name+"]", joinedLists(comparable(a[name])), joinedLists(comparable(b[name])))...)
	}
	return out
}
