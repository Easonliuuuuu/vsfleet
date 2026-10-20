package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/humanize"
)

// fieldLabelMax is the widest a field name gets in the History inspector and
// the timeline detail before it is cut, so a long device path cannot push
// its values off the screen.
const fieldLabelMax = 24

// historyFieldValue shares browse vocabulary while leaving stored evidence
// and exports intact: the store keeps memory in megabytes, disk sizes in
// bytes and times in RFC 3339, and History shows them the way the State
// block and the rest of the hub do. Other field values keep their original
// meaning.
func historyFieldValue(name, value string) string {
	if value == "" {
		return "—"
	}
	if unquoted, err := strconv.Unquote(value); err == nil && strings.HasPrefix(value, `"`) {
		value = unquoted
	}
	parent, leaf := fieldLeaf(name)
	switch {
	case leaf == "power_state" || leaf == "power":
		return powerWord(value)
	case leaf == "tools_state":
		return toolsWord(value, "")
	case leaf == "guest_state":
		return guestStateWord(value)
	case value == "true":
		return "yes"
	case value == "false":
		return "no"
	case name == "memory" || strings.HasSuffix(leaf, "_mb") || parent == "memory_allocation":
		return quantity(value, humanize.MB)
	case strings.HasSuffix(leaf, "_mhz") || parent == "cpu_allocation":
		return quantity(value, humanize.MHz)
	case strings.HasSuffix(leaf, "_bytes"):
		return quantity(value, humanize.Bytes)
	case strings.HasSuffix(leaf, "_gb"):
		if gb, err := strconv.ParseFloat(value, 64); err == nil && gb > 0 {
			return humanize.GB(gb)
		}
	}
	if at, err := time.Parse(time.RFC3339, value); err == nil {
		return at.Local().Format("2006-01-02 15:04")
	}
	return value
}

// quantity renders a stored integer in its unit. vSphere stores an unset
// limit as -1, and a zero reservation is a real setting, not a missing one.
func quantity(value string, unit func(int64) string) string {
	n, err := strconv.ParseInt(value, 10, 64)
	switch {
	case err != nil:
		return value
	case n < 0:
		return "unlimited"
	case n == 0:
		return "0"
	default:
		return unit(n)
	}
}

// fieldLeaf splits a stored field path into its last two names, without
// device keys: "migration_configuration.cpu_allocation.limit" is
// ("cpu_allocation", "limit").
func fieldLeaf(name string) (parent, leaf string) {
	parts := strings.Split(name, ".")
	leaf, _ = fieldSegment(parts[len(parts)-1])
	if len(parts) > 1 {
		parent, _ = fieldSegment(parts[len(parts)-2])
	}
	return parent, leaf
}

// fieldSegment splits "disks[2000]" into "disks" and "2000".
func fieldSegment(s string) (name, key string) {
	if i := strings.IndexByte(s, '['); i >= 0 && strings.HasSuffix(s, "]") {
		return s[:i], s[i+1 : len(s)-1]
	}
	return s, ""
}

// fieldNames are the stored names whose operator wording is not just the
// name with its underscores taken out.
var fieldNames = map[string]string{
	"cpu":                              "vCPU",
	"annotation":                       "notes",
	"power_state":                      "power",
	"tools_state":                      "VMware Tools",
	"storage_gb":                       "storage",
	"configuration_available":          "config readable",
	"secure_boot_enabled":              "secure boot",
	"auto_cores_per_socket":            "auto cores/socket",
	"cores_per_socket":                 "cores/socket",
	"memory_reservation_locked_to_max": "memory locked",
	"upt_compatibility_enabled":        "UPT",
	"raw":                              "RDM",
	"raw_lun_id":                       "RDM LUN",
	"raw_compatibility_mode":           "RDM mode",
	"capacity_bytes":                   "size",
	// Device lists and nested groups, which prefix the field inside them.
	"disks":             "disk",
	"nics":              "NIC",
	"tpms":              "TPM",
	"pci_devices":       "PCI device",
	"floppies":          "floppy",
	"cpu_allocation":    "CPU",
	"memory_allocation": "memory",
	"managed_by":        "managed by",
	"custom_attributes": "attribute",
	"tags":              "tag",
}

// fieldWords are the words that read as acronyms or in full.
var fieldWords = map[string]string{
	"cpu": "CPU", "mem": "memory", "vm": "VM", "vms": "VMs", "os": "OS", "id": "ID", "ip": "IP",
	"uuid": "UUID", "mac": "MAC", "mtu": "MTU", "drs": "DRS", "ha": "HA", "lacp": "LACP", "vlan": "VLAN",
}

// fieldLabel is the operator's name for a stored field path:
// "migration_configuration.cpu_sockets" is "CPU sockets" and
// "migration_configuration.disks[2000].capacity_bytes" is "disk 2000 size".
// A tag's key is two opaque IDs, so a tag reads as "tag" and its value names it.
func fieldLabel(path string) string {
	path = strings.TrimPrefix(path, "migration_configuration.")
	path = strings.TrimPrefix(path, "metadata.")
	var words []string
	for _, segment := range strings.Split(path, ".") {
		name, key := fieldSegment(segment)
		word, ok := fieldNames[name]
		if !ok {
			word = fieldWordsOf(name)
		}
		words = append(words, word)
		if key != "" && name != "tags" {
			words = append(words, key)
		}
	}
	return strings.Join(words, " ")
}

// fieldWordsOf spells a snake_case name as words, dropping a trailing unit
// that historyFieldValue already prints with the value.
func fieldWordsOf(name string) string {
	for _, unit := range []string{"_bytes", "_mhz", "_mb", "_gb"} {
		if trimmed, ok := strings.CutSuffix(name, unit); ok && trimmed != "" {
			name = trimmed
			break
		}
	}
	parts := strings.Split(name, "_")
	for i, part := range parts {
		if word, ok := fieldWords[part]; ok {
			parts[i] = word
		}
	}
	return strings.Join(parts, " ")
}

// fieldChangeLines lays out field changes as label, before and after, with
// every label padded to the widest one so the values line up. minLabel keeps
// the column in line with a block above it.
func fieldChangeLines(fields []assessment.FieldChange, minLabel int) []string {
	width := minLabel
	for _, f := range fields {
		width = max(width, len([]rune(fieldLabel(f.Field))))
	}
	width = min(width, fieldLabelMax)
	lines := make([]string, len(fields))
	for i, f := range fields {
		lines[i] = fmt.Sprintf("  %s %s → %s", pad(fieldLabel(f.Field), width, false), truncate(historyFieldValue(f.Field, f.Before), 24), truncate(historyFieldValue(f.Field, f.After), 24))
	}
	return lines
}

// fieldChangeSummary is field changes on one line, for the change stream and
// the timeline list: "vCPU:2→4 memory:4G→8G".
func fieldChangeSummary(fields []assessment.FieldChange) string {
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = fieldLabel(f.Field) + ":" + historyFieldValue(f.Field, f.Before) + "→" + historyFieldValue(f.Field, f.After)
	}
	return strings.Join(parts, " ")
}

// timelineKind is a stored event kind as words: "first_seen" is "first
// seen" and "snapshot-created" is "snapshot created".
func timelineKind(kind string) string {
	return strings.NewReplacer("_", " ", "-", " ").Replace(kind)
}
