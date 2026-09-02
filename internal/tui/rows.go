package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/easonliuuuuu/vc-tui/internal/humanize"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

// rowStatus drives the colour and glyph of a row, and nothing else. Keeping it
// separate from the domain objects means the UI decides what "worrying" looks
// like without the inventory layer growing opinions about presentation.
type rowStatus int

const (
	statusNone rowStatus = iota
	statusIdle
	statusGood
	statusWarn
	statusBad
)

// column describes one table column. A width of zero means the column absorbs
// whatever space the fixed ones leave over; exactly one column per kind is
// flexible, and it is always the name.
type column struct {
	title string
	width int
	right bool
}

// field is one label/value pair in a detail pane.
type field struct {
	label string
	value string
}

// row is one line in the resource table, already flattened. The table renderer
// knows nothing about virtual machines or datastores: each kind supplies its
// own columns, cells and detail fields, and everything below is generic.
type row struct {
	key     string
	context string
	name    string
	glyph   string
	status  rowStatus
	cells   []string
	detail  []field
	// notes are the free-form paragraphs under the detail fields, used for
	// annotations that would not survive being squeezed into a column.
	notes []field
}

// columnsFor returns the columns for a kind. withContext adds the leading
// vCenter column, which only earns its width when more than one is in view.
func columnsFor(kind vsphere.Kind, withContext bool) []column {
	var cols []column
	if withContext {
		cols = append(cols, column{title: "VCENTER", width: 14})
	}
	switch kind {
	case vsphere.KindVM:
		cols = append(cols,
			column{title: "NAME"},
			column{title: "POWER", width: 8},
			column{title: "CPU", width: 4, right: true},
			column{title: "MEM", width: 6, right: true},
			column{title: "IP ADDRESS", width: 16},
			column{title: "HOST", width: 18},
			column{title: "DATACENTER", width: 14},
		)
	case vsphere.KindTemplate:
		cols = append(cols,
			column{title: "NAME"},
			column{title: "GUEST OS", width: 28},
			column{title: "CPU", width: 4, right: true},
			column{title: "MEM", width: 6, right: true},
			column{title: "DISK", width: 8, right: true},
			column{title: "DATACENTER", width: 14},
		)
	case vsphere.KindHost:
		cols = append(cols,
			column{title: "NAME"},
			column{title: "STATE", width: 14},
			column{title: "CLUSTER", width: 18},
			column{title: "CPU", width: 10, right: true},
			column{title: "MEMORY", width: 10, right: true},
			column{title: "VMS", width: 5, right: true},
			column{title: "VERSION", width: 10},
		)
	case vsphere.KindCluster:
		cols = append(cols,
			column{title: "NAME"},
			column{title: "HOSTS", width: 7, right: true},
			column{title: "CORES", width: 7, right: true},
			column{title: "MEMORY", width: 10, right: true},
			column{title: "DRS", width: 5},
			column{title: "HA", width: 5},
			column{title: "DATACENTER", width: 14},
		)
	case vsphere.KindDatastore:
		cols = append(cols,
			column{title: "NAME"},
			column{title: "TYPE", width: 8},
			column{title: "CAPACITY", width: 10, right: true},
			column{title: "FREE", width: 10, right: true},
			column{title: "USED", width: 18},
			column{title: "DATACENTER", width: 14},
		)
	case vsphere.KindNetwork:
		cols = append(cols,
			column{title: "NAME"},
			column{title: "TYPE", width: 26},
			column{title: "ACCESSIBLE", width: 12},
			column{title: "DATACENTER", width: 14},
		)
	}
	return cols
}

// tabTitle is the label on the resource tab.
func tabTitle(kind vsphere.Kind) string {
	switch kind {
	case vsphere.KindVM:
		return "VMs"
	case vsphere.KindTemplate:
		return "Templates"
	case vsphere.KindHost:
		return "Hosts"
	case vsphere.KindCluster:
		return "Clusters"
	case vsphere.KindDatastore:
		return "Datastores"
	case vsphere.KindNetwork:
		return "Networks"
	default:
		return string(kind)
	}
}

// kindLabel names one object of a kind, for a detail pane where the plural tab
// title would be describing a single thing.
func kindLabel(kind vsphere.Kind) string {
	switch kind {
	case vsphere.KindVM:
		return "Virtual machine"
	case vsphere.KindTemplate:
		return "Template"
	case vsphere.KindHost:
		return "ESXi host"
	case vsphere.KindCluster:
		return "Cluster"
	case vsphere.KindDatastore:
		return "Datastore"
	case vsphere.KindNetwork:
		return "Network"
	default:
		return string(kind)
	}
}

// countFor reports how many objects of a kind an inventory holds, so the tab
// bar can carry counts without every tab building its rows.
func countFor(inv *vsphere.Inventory, kind vsphere.Kind) int {
	if inv == nil {
		return 0
	}
	switch kind {
	case vsphere.KindVM:
		return len(inv.VMs)
	case vsphere.KindTemplate:
		return len(inv.Templates)
	case vsphere.KindHost:
		return len(inv.Hosts)
	case vsphere.KindCluster:
		return len(inv.Clusters)
	case vsphere.KindDatastore:
		return len(inv.Datastores)
	case vsphere.KindNetwork:
		return len(inv.Networks)
	default:
		return 0
	}
}

// rowsFor flattens one inventory into table rows for a kind.
func rowsFor(inv *vsphere.Inventory, kind vsphere.Kind, withContext bool) []row {
	if inv == nil {
		return nil
	}
	var out []row
	switch kind {
	case vsphere.KindVM:
		for _, vm := range inv.VMs {
			out = append(out, vmRow(vm, withContext))
		}
	case vsphere.KindTemplate:
		for _, vm := range inv.Templates {
			out = append(out, templateRow(vm, withContext))
		}
	case vsphere.KindHost:
		for _, h := range inv.Hosts {
			out = append(out, hostRow(h, withContext))
		}
	case vsphere.KindCluster:
		for _, c := range inv.Clusters {
			out = append(out, clusterRow(c, withContext))
		}
	case vsphere.KindDatastore:
		for _, d := range inv.Datastores {
			out = append(out, datastoreRow(d, withContext))
		}
	case vsphere.KindNetwork:
		for _, n := range inv.Networks {
			out = append(out, networkRow(n, withContext))
		}
	}
	return out
}

func lead(withContext bool, ctxName string, cells ...string) []string {
	if withContext {
		return append([]string{ctxName}, cells...)
	}
	return cells
}

func vmRow(vm vsphere.VM, withContext bool) row {
	st := statusIdle
	glyph := glyphOffline
	switch vm.PowerState {
	case "poweredOn":
		st, glyph = statusGood, glyphOnline
	case "suspended":
		st, glyph = statusWarn, glyphPending
	}
	r := row{
		key:     vm.Context + "/" + vm.ID,
		context: vm.Context,
		name:    vm.Name,
		glyph:   glyph,
		status:  st,
		cells: lead(withContext, vm.Context,
			vm.Name,
			powerWord(vm.PowerState),
			strconv.FormatInt(int64(vm.CPU), 10),
			humanize.MB(vm.MemoryMB),
			humanize.Dash(vm.IPAddress),
			humanize.Dash(vm.Host),
			humanize.Dash(vm.Datacenter),
		),
		detail: []field{
			{"vCenter", vm.Context},
			{"Power state", powerWord(vm.PowerState)},
			{"Guest OS", humanize.Dash(vm.GuestOS)},
			{"Guest state", humanize.Dash(vm.GuestState)},
			{"VMware Tools", humanize.Dash(vm.ToolsState)},
			{"IP address", humanize.Dash(vm.IPAddress)},
			{"CPU", strconv.FormatInt(int64(vm.CPU), 10) + " vCPU"},
			{"Memory", humanize.MB(vm.MemoryMB)},
			{"Committed storage", humanize.GB(vm.StorageGB)},
			{"Host", humanize.Dash(vm.Host)},
			{"Cluster", humanize.Dash(vm.Cluster)},
			{"Datacenter", humanize.Dash(vm.Datacenter)},
			{"Folder", humanize.Dash(vm.Folder)},
			{"Datastores", humanize.Dash(strings.Join(vm.Datastores, ", "))},
			{"Inventory path", humanize.Dash(vm.Path)},
			{"Managed object", vm.ID},
		},
	}
	if vm.Annotation != "" {
		r.notes = append(r.notes, field{"Notes", vm.Annotation})
	}
	return r
}

func templateRow(vm vsphere.VM, withContext bool) row {
	return row{
		key:     vm.Context + "/" + vm.ID,
		context: vm.Context,
		name:    vm.Name,
		glyph:   glyphSkip,
		status:  statusNone,
		cells: lead(withContext, vm.Context,
			vm.Name,
			humanize.Dash(vm.GuestOS),
			strconv.FormatInt(int64(vm.CPU), 10),
			humanize.MB(vm.MemoryMB),
			humanize.GB(vm.StorageGB),
			humanize.Dash(vm.Datacenter),
		),
		detail: []field{
			{"vCenter", vm.Context},
			{"Guest OS", humanize.Dash(vm.GuestOS)},
			{"CPU", strconv.FormatInt(int64(vm.CPU), 10) + " vCPU"},
			{"Memory", humanize.MB(vm.MemoryMB)},
			{"Committed storage", humanize.GB(vm.StorageGB)},
			{"Datacenter", humanize.Dash(vm.Datacenter)},
			{"Folder", humanize.Dash(vm.Folder)},
			{"Datastores", humanize.Dash(strings.Join(vm.Datastores, ", "))},
			{"Inventory path", humanize.Dash(vm.Path)},
			{"Managed object", vm.ID},
		},
		notes: noteOf(vm.Annotation),
	}
}

func hostRow(h vsphere.Host, withContext bool) row {
	st, glyph := statusGood, glyphOnline
	state := h.ConnectionState
	switch {
	case h.InMaintenance:
		st, glyph, state = statusWarn, glyphPending, "maintenance"
	case h.ConnectionState != "connected":
		st, glyph = statusBad, glyphFail
	}
	return row{
		key:     h.Context + "/" + h.ID,
		context: h.Context,
		name:    h.Name,
		glyph:   glyph,
		status:  st,
		cells: lead(withContext, h.Context,
			h.Name,
			state,
			humanize.Dash(h.Cluster),
			ratio(humanize.MHz(h.CPUUsageMHz), humanize.MHz(int64(h.CPUCores)*int64(h.CPUMHz))),
			ratio(humanize.MB(h.MemoryUsageMB), humanize.MB(h.MemoryMB)),
			strconv.Itoa(h.VMCount),
			humanize.Dash(h.Version),
		),
		detail: []field{
			{"vCenter", h.Context},
			{"Connection", state},
			{"Power state", powerWord(h.PowerState)},
			{"Maintenance mode", yesNo(h.InMaintenance)},
			{"Cluster", humanize.Dash(h.Cluster)},
			{"Hardware", humanize.Dash(strings.TrimSpace(h.Vendor + " " + h.Model))},
			{"ESXi", humanize.Dash(strings.TrimSpace(h.Version + " build-" + h.Build))},
			{"CPU", fmt.Sprintf("%d cores / %d threads @ %s", h.CPUCores, h.CPUThreads, humanize.MHz(int64(h.CPUMHz)))},
			{"CPU used", ratio(humanize.MHz(h.CPUUsageMHz), humanize.MHz(int64(h.CPUCores)*int64(h.CPUMHz)))},
			{"Memory", humanize.MB(h.MemoryMB)},
			{"Memory used", ratio(humanize.MB(h.MemoryUsageMB), humanize.MB(h.MemoryMB))},
			{"Virtual machines", strconv.Itoa(h.VMCount)},
			{"Datacenter", humanize.Dash(h.Datacenter)},
			{"Inventory path", humanize.Dash(h.Path)},
			{"Managed object", h.ID},
		},
	}
}

func clusterRow(c vsphere.Cluster, withContext bool) row {
	st, glyph := statusGood, glyphOnline
	if c.Hosts == 0 {
		st, glyph = statusWarn, glyphOffline
	}
	kind := "Cluster"
	if c.Standalone {
		kind = "Standalone host"
	}
	return row{
		key:     c.Context + "/" + c.ID,
		context: c.Context,
		name:    c.Name,
		glyph:   glyph,
		status:  st,
		cells: lead(withContext, c.Context,
			c.Name,
			strconv.Itoa(c.Hosts),
			strconv.FormatInt(int64(c.CPUCores), 10),
			humanize.MB(c.TotalMemoryMB),
			yesNo(c.DRSEnabled),
			yesNo(c.HAEnabled),
			humanize.Dash(c.Datacenter),
		),
		detail: []field{
			{"vCenter", c.Context},
			{"Kind", kind},
			{"Hosts", strconv.Itoa(c.Hosts)},
			{"Effective hosts", strconv.Itoa(c.EffectiveHost)},
			{"CPU cores", strconv.FormatInt(int64(c.CPUCores), 10)},
			{"Total CPU", humanize.MHz(c.TotalCPUMHz)},
			{"Total memory", humanize.MB(c.TotalMemoryMB)},
			{"DRS", yesNo(c.DRSEnabled)},
			{"HA", yesNo(c.HAEnabled)},
			{"Datacenter", humanize.Dash(c.Datacenter)},
			{"Inventory path", humanize.Dash(c.Path)},
			{"Managed object", c.ID},
		},
	}
}

func datastoreRow(d vsphere.Datastore, withContext bool) row {
	st, glyph := statusGood, glyphOnline
	switch {
	case !d.Accessible:
		st, glyph = statusBad, glyphFail
	case d.UsedPercent() >= 90:
		st, glyph = statusBad, glyphOnline
	case d.UsedPercent() >= 75:
		st, glyph = statusWarn, glyphOnline
	}
	return row{
		key:     d.Context + "/" + d.ID,
		context: d.Context,
		name:    d.Name,
		glyph:   glyph,
		status:  st,
		cells: lead(withContext, d.Context,
			d.Name,
			humanize.Dash(d.Type),
			humanize.Bytes(d.CapacityBytes),
			humanize.Bytes(d.FreeBytes),
			usageBar(d.UsedPercent(), 10),
			humanize.Dash(d.Datacenter),
		),
		detail: []field{
			{"vCenter", d.Context},
			{"Type", humanize.Dash(d.Type)},
			{"Accessible", yesNo(d.Accessible)},
			{"Capacity", humanize.Bytes(d.CapacityBytes)},
			{"Used", fmt.Sprintf("%s (%.0f%%)", humanize.Bytes(d.UsedBytes()), d.UsedPercent())},
			{"Free", humanize.Bytes(d.FreeBytes)},
			{"Maintenance", humanize.Dash(d.Maintenance)},
			{"Datacenter", humanize.Dash(d.Datacenter)},
			{"Inventory path", humanize.Dash(d.Path)},
			{"Managed object", d.ID},
		},
	}
}

func networkRow(n vsphere.Network, withContext bool) row {
	st, glyph := statusGood, glyphOnline
	if !n.Accessible {
		st, glyph = statusBad, glyphFail
	}
	return row{
		key:     n.Context + "/" + n.ID,
		context: n.Context,
		name:    n.Name,
		glyph:   glyph,
		status:  st,
		cells: lead(withContext, n.Context,
			n.Name,
			humanize.Dash(n.Type),
			yesNo(n.Accessible),
			humanize.Dash(n.Datacenter),
		),
		detail: []field{
			{"vCenter", n.Context},
			{"Type", humanize.Dash(n.Type)},
			{"Accessible", yesNo(n.Accessible)},
			{"Datacenter", humanize.Dash(n.Datacenter)},
			{"Inventory path", humanize.Dash(n.Path)},
			{"Managed object", n.ID},
		},
	}
}

func noteOf(s string) []field {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return []field{{"Notes", s}}
}

// powerWord turns vSphere's poweredOn into the word an operator would say.
func powerWord(s string) string {
	switch s {
	case "poweredOn":
		return "on"
	case "poweredOff":
		return "off"
	case "suspended":
		return "suspended"
	case "":
		return "-"
	default:
		return s
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func ratio(used, total string) string {
	if total == "-" {
		return "-"
	}
	if used == "-" {
		used = "0"
	}
	return used + "/" + total
}

// usageBar renders a percentage as a bar plus the number. The bar is for the
// glance and the number is for the decision; neither is enough alone.
func usageBar(pct float64, width int) string {
	if pct <= 0 {
		return "-"
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct/100*float64(width) + 0.5)
	if filled == 0 {
		filled = 1
	}
	return strings.Repeat("█", filled) + strings.Repeat("·", width-filled) + fmt.Sprintf(" %3.0f%%", pct)
}
