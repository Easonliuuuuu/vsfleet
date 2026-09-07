package vsphere

import (
	"sort"
	"strings"
)

// IdentityKeys returns the strong, namespaced backing identities for a
// datastore. A local datastore deliberately returns no keys: it is local to
// the context that presents it and must never join two vCenters.
func (d Datastore) IdentityKeys() []string {
	if d.Backing.Local {
		return nil
	}
	keys := make([]string, 0, 2+len(d.Backing.Extents))
	if value := strings.TrimSpace(d.Backing.VMFSUUID); value != "" {
		keys = append(keys, "vmfs:"+strings.ToLower(value))
	}
	for _, extent := range d.Backing.Extents {
		if value := strings.TrimSpace(extent); value != "" {
			keys = append(keys, "extent:"+strings.ToLower(value))
		}
	}
	if value := strings.TrimSpace(d.Backing.NASRemote); value != "" {
		keys = append(keys, "nas:"+strings.ToLower(value))
	}
	if value := strings.TrimSpace(d.Backing.VVolID); value != "" {
		keys = append(keys, "vvol:"+strings.ToLower(value))
	}
	if value := NormalizeBackingURL(d.Backing.URL); value != "" {
		keys = append(keys, "url:"+value)
	}
	return uniqueIdentityStrings(keys)
}

// NormalizeBackingURL normalizes a datastore backing URL for identity joins.
func NormalizeBackingURL(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, "\\", "/")))
	return strings.TrimRight(value, "/")
}

// SplitDatastorePath splits a canonical [datastore] relative path.
func SplitDatastorePath(value string) (string, string, bool) {
	normalized := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if !strings.HasPrefix(normalized, "[") {
		return "", "", false
	}
	close := strings.IndexByte(normalized, ']')
	if close <= 1 {
		return "", "", false
	}
	name := strings.ToLower(strings.TrimSpace(normalized[1:close]))
	relative := NormalizeRelativePath(normalized[close+1:])
	return name, relative, true
}

// NormalizeRelativePath normalizes a path relative to a datastore.
func NormalizeRelativePath(value string) string {
	value = strings.Trim(strings.TrimSpace(strings.ReplaceAll(value, "\\", "/")), "/")
	for strings.Contains(value, "//") {
		value = strings.ReplaceAll(value, "//", "/")
	}
	return strings.ToLower(value)
}

func uniqueIdentityStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok || value == "" {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
