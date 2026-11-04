package tui

import (
	"cmp"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// sortOrder is how one table is ordered: which of its sortKeys, and which
// way. The zero value is the default, name order.
type sortOrder struct {
	key  string
	desc bool
}

// sortKey is one way to order a kind's table. Each tab offers its own,
// listed in the sort menu in the order of the columns they follow.
type sortKey struct {
	id    string
	label string
	// letter picks the key straight from the sort menu. None is "r", "s",
	// "j" or "k", which the menu keeps for itself.
	letter string
	// column is the heading the key orders by, which the table marks with
	// the direction. Empty for an order no single column shows.
	column string
	// desc is the direction the key starts in: largest first for a number,
	// A to Z for text.
	desc  bool
	value func(row) sortValue
}

// sortValue is one row's value under a sortKey. A value that is not
// reported (ok false) sorts last whichever way the table runs, so reversing
// never brings a column of dashes to the top.
type sortValue struct {
	ok      bool
	num     float64
	text    string
	version []int
	ip      netip.Addr
}

func numValue(n float64, ok bool) sortValue { return sortValue{ok: ok, num: n} }

func textValue(s string) sortValue { return sortValue{ok: s != "", text: s} }

func rankValue(n int) sortValue { return sortValue{ok: true, num: float64(n)} }

// versionValue reads "8.0.3" as its numeric parts, so 8.0.10 follows 8.0.3.
func versionValue(s string) sortValue {
	if s == "" {
		return sortValue{}
	}
	var parts []int
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r < '0' || r > '9' }) {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		parts = append(parts, n)
	}
	return sortValue{ok: true, version: parts, text: s}
}

// ipValue orders addresses numerically, IPv4 before IPv6. Anything that does
// not parse falls back to text order after them.
func ipValue(s string) sortValue {
	if s == "" {
		return sortValue{}
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return sortValue{ok: true, text: s}
	}
	return sortValue{ok: true, ip: a.Unmap()}
}

func compareValues(a, b sortValue) int {
	switch {
	case a.version != nil || b.version != nil:
		if c := slices.Compare(a.version, b.version); c != 0 {
			return c
		}
		return naturalCompare(a.text, b.text)
	case a.ip.IsValid() || b.ip.IsValid():
		switch {
		case !b.ip.IsValid():
			return -1
		case !a.ip.IsValid():
			return 1
		}
		return a.ip.Compare(b.ip)
	case a.text != "" || b.text != "":
		return naturalCompare(a.text, b.text)
	}
	return cmp.Compare(a.num, b.num)
}

// naturalCompare orders names the way a person reads them: case aside, and
// with runs of digits compared as numbers, so web-2 comes before web-10.
// Names equal on those terms fall back to byte order, so the order is total.
func naturalCompare(a, b string) int {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	i, j := 0, 0
	for i < len(la) && j < len(lb) {
		ca, cb := la[i], lb[j]
		if isDigit(ca) && isDigit(cb) {
			si, sj := i, j
			for i < len(la) && isDigit(la[i]) {
				i++
			}
			for j < len(lb) && isDigit(lb[j]) {
				j++
			}
			na := strings.TrimLeft(la[si:i], "0")
			nb := strings.TrimLeft(lb[sj:j], "0")
			if c := cmp.Compare(len(na), len(nb)); c != 0 {
				return c
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			continue
		}
		if ca != cb {
			return cmp.Compare(ca, cb)
		}
		i++
		j++
	}
	if c := cmp.Compare(len(la)-i, len(lb)-j); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

var (
	nameKey = sortKey{id: "name", label: "name", letter: "n", column: "NAME",
		value: func(r row) sortValue { return textValue(r.name) }}
	// statusKey is the problems-first order every kind with a status had
	// before columns could be sorted.
	statusKey = sortKey{id: "status", label: "status", letter: "t",
		value: func(r row) sortValue { return rankValue(statusRank(r.status)) }}
	datacenterKey = sortKey{id: "datacenter", label: "datacenter", letter: "d", column: "DATACENTER",
		value: func(r row) sortValue { return textValue(r.where.Datacenter) }}
)

// vmValue and the helpers after it read a sort value from the object a row
// was built from, with nothing to report for a row that has none.
func vmValue(f func(vsphere.VM) sortValue) func(row) sortValue {
	return func(r row) sortValue {
		if r.filed == nil {
			return sortValue{}
		}
		return f(*r.filed)
	}
}

func hostValue(f func(vsphere.Host) sortValue) func(row) sortValue {
	return func(r row) sortValue {
		if r.esxi == nil {
			return sortValue{}
		}
		return f(*r.esxi)
	}
}

func clusterValue(f func(vsphere.Cluster) sortValue) func(row) sortValue {
	return func(r row) sortValue {
		if r.cluster == nil {
			return sortValue{}
		}
		return f(*r.cluster)
	}
}

func datastoreValue(f func(vsphere.Datastore) sortValue) func(row) sortValue {
	return func(r row) sortValue {
		if r.datastore == nil {
			return sortValue{}
		}
		return f(*r.datastore)
	}
}

// usedShare is used/total as a percentage, unreported without a total, the
// way capacityBar draws it.
func usedShare(used, total int64) sortValue {
	if total <= 0 {
		return sortValue{}
	}
	if used < 0 {
		used = 0
	}
	return numValue(float64(used)/float64(total)*100, true)
}

var (
	vmCPUKey = sortKey{id: "cpu", label: "CPU", letter: "c", column: "CPU", desc: true,
		value: vmValue(func(v vsphere.VM) sortValue { return numValue(float64(v.CPU), v.CPU > 0) })}
	vmMemoryKey = sortKey{id: "memory", label: "memory", letter: "m", column: "MEM", desc: true,
		value: vmValue(func(v vsphere.VM) sortValue { return numValue(float64(v.MemoryMB), v.MemoryMB > 0) })}
)

// sortKeysFor lists the orders a kind's table offers, name first. Every
// kind has at least name; the menu is what offers the rest.
func sortKeysFor(kind vsphere.Kind) []sortKey {
	switch kind {
	case vsphere.KindVM:
		return []sortKey{
			nameKey,
			// Power is the problems-first order VMs always had: suspended,
			// then on, then off.
			{id: "power", label: "power", letter: "p", column: "POWER",
				value: statusKey.value},
			vmCPUKey,
			vmMemoryKey,
			{id: "ip", label: "IP address", letter: "i", column: "IP ADDRESS",
				value: vmValue(func(v vsphere.VM) sortValue { return ipValue(v.IPAddress) })},
			{id: "host", label: "host", letter: "h", column: "HOST",
				value: vmValue(func(v vsphere.VM) sortValue { return textValue(v.Host) })},
			datacenterKey,
		}
	case vsphere.KindTemplate:
		return []sortKey{
			nameKey,
			{id: "guest", label: "guest OS", letter: "g", column: "GUEST OS",
				value: vmValue(func(v vsphere.VM) sortValue { return textValue(v.GuestOS) })},
			vmCPUKey,
			vmMemoryKey,
			{id: "disk", label: "disk", letter: "i", column: "DISK", desc: true,
				value: vmValue(func(v vsphere.VM) sortValue { return numValue(v.StorageGB, v.StorageGB > 0) })},
			datacenterKey,
		}
	case vsphere.KindHost:
		return []sortKey{
			nameKey,
			{id: "status", label: "state", letter: "t", column: "STATE",
				value: statusKey.value},
			{id: "cluster", label: "cluster", letter: "l", column: "CLUSTER",
				value: hostValue(func(h vsphere.Host) sortValue { return textValue(h.Cluster) })},
			{id: "cpu", label: "CPU used", letter: "c", column: "CPU", desc: true,
				value: hostValue(func(h vsphere.Host) sortValue { return usedShare(h.CPUUsageMHz, h.TotalCPU()) })},
			{id: "memory", label: "memory used", letter: "m", column: "MEMORY", desc: true,
				value: hostValue(func(h vsphere.Host) sortValue { return usedShare(h.MemoryUsageMB, h.MemoryMB) })},
			{id: "vms", label: "VMs", letter: "v", column: "VMS", desc: true,
				value: hostValue(func(h vsphere.Host) sortValue { return numValue(float64(h.VMCount), true) })},
			{id: "version", label: "version", letter: "e", column: "VERSION",
				value: hostValue(func(h vsphere.Host) sortValue { return versionValue(h.Version) })},
		}
	case vsphere.KindCluster:
		return []sortKey{
			nameKey,
			statusKey,
			{id: "hosts", label: "hosts", letter: "h", column: "HOSTS", desc: true,
				value: clusterValue(func(c vsphere.Cluster) sortValue { return numValue(float64(c.Hosts), true) })},
			{id: "cores", label: "cores", letter: "c", column: "CORES", desc: true,
				value: clusterValue(func(c vsphere.Cluster) sortValue { return numValue(float64(c.CPUCores), c.CPUCores > 0) })},
			{id: "memory", label: "memory", letter: "m", column: "MEMORY", desc: true,
				value: clusterValue(func(c vsphere.Cluster) sortValue {
					return numValue(float64(c.TotalMemoryMB), c.TotalMemoryMB > 0)
				})},
			// On first, the way a number starts largest first.
			{id: "drs", label: "DRS", letter: "x", column: "DRS", desc: true,
				value: clusterValue(func(c vsphere.Cluster) sortValue { return numValue(boolNum(c.DRSEnabled), true) })},
			{id: "ha", label: "HA", letter: "a", column: "HA", desc: true,
				value: clusterValue(func(c vsphere.Cluster) sortValue { return numValue(boolNum(c.HAEnabled), true) })},
			datacenterKey,
		}
	case vsphere.KindDatastore:
		return []sortKey{
			nameKey,
			statusKey,
			{id: "type", label: "type", letter: "y", column: "TYPE",
				value: datastoreValue(func(d vsphere.Datastore) sortValue { return textValue(d.Type) })},
			{id: "capacity", label: "capacity", letter: "c", column: "CAPACITY", desc: true,
				value: datastoreValue(func(d vsphere.Datastore) sortValue {
					return numValue(float64(d.CapacityBytes), d.CapacityBytes > 0)
				})},
			{id: "free", label: "free", letter: "f", column: "FREE", desc: true,
				value: datastoreValue(func(d vsphere.Datastore) sortValue {
					return numValue(float64(d.FreeBytes), d.CapacityBytes > 0)
				})},
			{id: "used", label: "used %", letter: "u", column: "USED", desc: true,
				value: datastoreValue(func(d vsphere.Datastore) sortValue {
					return numValue(d.UsedPercent(), d.CapacityBytes > 0)
				})},
			datacenterKey,
		}
	default:
		// vApps and Networks are mostly labels; the problems-first order is
		// the one worth having beside their names.
		return []sortKey{nameKey, statusKey}
	}
}

func boolNum(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// findSortKey is the key an order names among keys, or name order for one
// this table does not offer — a remembered order for a column since removed,
// say.
func findSortKey(keys []sortKey, id string) sortKey {
	for _, k := range keys {
		if k.id == id {
			return k
		}
	}
	return keys[0]
}

// resolve is o as keys can apply it: the key itself, and the direction,
// which for an unknown key is name order's own.
func (o sortOrder) resolve(keys []sortKey) (sortKey, bool) {
	k := findSortKey(keys, o.key)
	if k.id != o.key {
		return k, k.desc
	}
	return k, o.desc
}

// sortRows orders rows in place by o. Name order keeps the rows of each
// vCenter together, in the order they arrived, because that grouping is
// itself information once more than one is in scope. Every other order
// mixes them and breaks ties by name, then vCenter, so a refresh that
// changes nothing changes nothing on screen.
func sortRows(rows []row, keys []sortKey, o sortOrder) {
	k, desc := o.resolve(keys)
	group := map[string]int{}
	for _, r := range rows {
		if _, ok := group[r.context]; !ok {
			group[r.context] = len(group)
		}
	}
	byName := func(a, b row) int {
		if c := cmp.Compare(group[a.context], group[b.context]); c != 0 {
			return c
		}
		return naturalCompare(a.name, b.name)
	}
	if k.id == nameKey.id {
		slices.SortStableFunc(rows, func(a, b row) int {
			if c := cmp.Compare(group[a.context], group[b.context]); c != 0 {
				return c
			}
			c := naturalCompare(a.name, b.name)
			if desc {
				return -c
			}
			return c
		})
		return
	}
	slices.SortStableFunc(rows, func(a, b row) int {
		va, vb := k.value(a), k.value(b)
		switch {
		case va.ok && !vb.ok:
			return -1
		case !va.ok && vb.ok:
			return 1
		case va.ok && vb.ok:
			c := compareValues(va, vb)
			if desc {
				c = -c
			}
			if c != 0 {
				return c
			}
		}
		if c := naturalCompare(a.name, b.name); c != 0 {
			return c
		}
		return byName(a, b)
	})
}

// sortFor is the order the given kind's table is in.
func (m *Model) sortFor(kind vsphere.Kind) sortOrder { return m.sorts[kind] }

// applySort orders rows by the current tab's order.
func (m *Model) applySort(rows []row) {
	sortRows(rows, sortKeysFor(m.kind), m.sortFor(m.kind))
}

// setSort puts the current tab in o and brings the table back to its top,
// where the order's answer is.
func (m *Model) setSort(o sortOrder) {
	keys := sortKeysFor(m.kind)
	k := findSortKey(keys, o.key)
	o.key = k.id
	if o.key == nameKey.id && o.desc == nameKey.desc {
		delete(m.sorts, m.kind)
	} else {
		m.sorts[m.kind] = o
	}
	m.cursor, m.offset = 0, 0
}

// reverseSort flips the current tab's direction, the browse screen's "S".
func (m *Model) reverseSort() {
	k, desc := m.sortFor(m.kind).resolve(sortKeysFor(m.kind))
	m.setSort(sortOrder{key: k.id, desc: !desc})
}

// sortLabel is the header's account of the current order, "sort: memory ↓".
// Plain name order, the default, carries no arrow.
func (m *Model) sortLabel() string {
	k, desc := m.sortFor(m.kind).resolve(sortKeysFor(m.kind))
	if k.id == nameKey.id && !desc {
		return "sort: name"
	}
	return "sort: " + k.label + " " + sortArrow(desc)
}

func sortArrow(desc bool) string {
	if desc {
		return "↓"
	}
	return "↑"
}

// sortedColumn is the heading the current order follows and its direction,
// for the table to mark; empty when no column shows it.
func (m *Model) sortedColumn() (string, bool) {
	k, desc := m.sortFor(m.kind).resolve(sortKeysFor(m.kind))
	if k.id == nameKey.id && !desc {
		return "", false
	}
	return k.column, desc
}

// encodeSorts and decodeSorts carry the per-tab orders across runs as
// "kind": "key:asc|desc". Name order is left out: it is the default.
func encodeSorts(sorts map[vsphere.Kind]sortOrder) map[string]string {
	if len(sorts) == 0 {
		return nil
	}
	out := make(map[string]string, len(sorts))
	for kind, o := range sorts {
		dir := "asc"
		if o.desc {
			dir = "desc"
		}
		out[string(kind)] = o.key + ":" + dir
	}
	return out
}

func decodeSorts(saved map[string]string, legacy string) map[vsphere.Kind]sortOrder {
	out := map[vsphere.Kind]sortOrder{}
	if len(saved) == 0 && legacy == statusKey.id {
		// Before each tab kept its own order, "status" was one setting for
		// every tab. Each tab with a problems-first order keeps it; VMs read
		// it as power, which is what it sorted them by.
		for _, kind := range vsphere.AllKinds {
			keys := sortKeysFor(kind)
			switch {
			case kind == vsphere.KindVM:
				out[kind] = sortOrder{key: "power"}
			case findSortKey(keys, statusKey.id).id == statusKey.id:
				out[kind] = sortOrder{key: statusKey.id}
			}
		}
		return out
	}
	for kindName, v := range saved {
		kind := vsphere.Kind(kindName)
		if !slices.Contains(vsphere.AllKinds, kind) {
			continue
		}
		id, dir, _ := strings.Cut(v, ":")
		keys := sortKeysFor(kind)
		k := findSortKey(keys, id)
		if k.id != id {
			continue
		}
		desc := k.desc
		switch dir {
		case "asc":
			desc = false
		case "desc":
			desc = true
		}
		out[kind] = sortOrder{key: k.id, desc: desc}
	}
	return out
}
