package topology

import "strings"

// networkIdentity carries the strong joins that are not part of the small
// persisted Network wire object. Distributed port groups gain a composite
// (switch UUID, port-group key) identity; NSX observations use their logical
// switch or segment identifiers.
type networkIdentity struct {
	keys          []string
	portGroupKey  string
	switchUUID    string
	logicalSwitch string
	segmentID     string
	vlan          string
}

func networkKeys(identity networkIdentity) []string {
	keys := make([]string, 0, 3)
	if identity.portGroupKey != "" && identity.switchUUID != "" {
		keys = append(keys, "portgroup:"+strings.ToLower(strings.TrimSpace(identity.switchUUID))+":"+strings.ToLower(strings.TrimSpace(identity.portGroupKey)))
	}
	if identity.logicalSwitch != "" {
		keys = append(keys, "logical-switch:"+strings.ToLower(strings.TrimSpace(identity.logicalSwitch)))
	}
	if identity.segmentID != "" {
		keys = append(keys, "segment:"+strings.ToLower(strings.TrimSpace(identity.segmentID)))
	}
	return keys
}

func uniqueLower(values []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
