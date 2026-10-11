package tui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/humanize"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The VMs and Templates tabs can be browsed by folder, one level at a time,
// the way the vSphere client's VMs and Templates view is: enter opens a
// folder, esc goes back up, and the message line says where you are. Each
// tab remembers its own folder. A folder is not an inventory object here; it
// is derived from the Folder every VM and template already carries, which is
// relative to its datacenter's vm folder ("/Prod/Web"). A VM inside a vApp
// lists the vApp as its folder, so a vApp appears as one more level.

// glyphFolder marks a folder row: the closed group of the Networks tree,
// since ▸ is the cursor.
const glyphFolder = foldClosed

// folderSpot is a place in the folder view: a folder of one datacenter in
// one vCenter, or, when datacenter is empty, the level that lists every
// datacenter in scope.
type folderSpot struct {
	context    string
	datacenter string
	path       string
}

// root reports whether the spot is the level that lists datacenters.
func (s folderSpot) root() bool { return s.datacenter == "" }

// holds reports whether r sits in the spot's folder or anywhere below it.
func (s folderSpot) holds(r row) bool {
	if s.root() {
		return true
	}
	if r.context != s.context || r.where.Datacenter != s.datacenter {
		return false
	}
	f := rowFolder(r)
	return s.path == "/" || f == s.path || strings.HasPrefix(f, s.path+"/")
}

// rowFolder is the folder a VM or template row is filed in.
func rowFolder(r row) string {
	if r.filed == nil || r.filed.Folder == "" {
		return "/"
	}
	return r.filed.Folder
}

// spotOf is the folder a row sits in.
func spotOf(r row) folderSpot {
	return folderSpot{context: r.context, datacenter: r.where.Datacenter, path: rowFolder(r)}
}

// foldersOffered reports whether a kind can be browsed by folder.
func foldersOffered(k vsphere.Kind) bool {
	return k == vsphere.KindVM || k == vsphere.KindTemplate
}

// folderView reports whether the current tab is showing folders.
func (m *Model) folderView() bool {
	return foldersOffered(m.kind) && m.folderOn[m.kind]
}

// toggleFolderView switches the tab between folders and the plain list.
// Folders open at the top, so the first thing shown is the folders
// themselves; going back to the list keeps the selected row.
func (m *Model) toggleFolderView() {
	if m.folderOn == nil {
		m.folderOn = map[vsphere.Kind]bool{}
	}
	if m.folderOn[m.kind] {
		m.preserveCursor(func() { m.folderOn[m.kind] = false })
		return
	}
	m.folderOn[m.kind] = true
	m.setFolder(folderSpot{})
	m.cursor, m.offset = 0, 0
}

func (m *Model) setFolder(s folderSpot) {
	if m.folderAt == nil {
		m.folderAt = map[vsphere.Kind]folderSpot{}
	}
	m.folderAt[m.kind] = s
}

// folderSource is every row of the current tab in scope, unfiltered.
func (m *Model) folderSource() []row {
	var all []row
	for _, st := range m.inScope() {
		all = append(all, st.rowsFor(m.kind, m.showContext())...)
	}
	return all
}

// currentFolder resolves the remembered spot against what is in scope: a
// datacenter that has left the scope falls back to the root, and a scope
// with one datacenter starts inside it, since a level listing one entry
// would only cost a keypress.
func (m *Model) currentFolder(all []row) folderSpot {
	spot := m.folderAt[m.kind]
	dcs := folderDatacenters(all)
	for _, dc := range dcs {
		if !spot.root() && dc.context == spot.context && dc.datacenter == spot.datacenter {
			return spot
		}
	}
	if len(dcs) == 1 {
		return dcs[0]
	}
	return folderSpot{}
}

// folderDatacenters lists the datacenters rows sit in, each as the spot of
// its top folder, in vCenter then datacenter order.
func folderDatacenters(rows []row) []folderSpot {
	seen := map[folderSpot]bool{}
	var out []folderSpot
	for _, r := range rows {
		s := folderSpot{context: r.context, datacenter: r.where.Datacenter, path: "/"}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].context != out[j].context {
			return out[i].context < out[j].context
		}
		return out[i].datacenter < out[j].datacenter
	})
	return out
}

// folderRows is rows() for a tab showing folders. A filter searches the
// current folder and everything under it, and lists the matches flat, since
// the point of typing is to find a VM without opening folders. A jump from
// another object lists all its matches flat, wherever they are filed.
func (m *Model) folderRows() []row {
	all := m.folderSource()
	spot := m.currentFolder(all)
	needle := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	jump := m.jump
	if jump != nil && jump.kind != m.kind {
		jump = nil
	}
	if needle != "" || jump != nil {
		var out []row
		for _, r := range all {
			switch {
			case jump == nil && !spot.holds(r):
			case needle != "" && !strings.Contains(strings.ToLower(r.name), needle):
			case jump != nil && !jump.matches(r):
			default:
				out = append(out, r)
			}
		}
		m.sortMode.apply(out)
		return out
	}

	if spot.root() {
		tally := map[folderSpot]*folderTally{}
		for _, r := range all {
			dc := folderSpot{context: r.context, datacenter: r.where.Datacenter, path: "/"}
			if tally[dc] == nil {
				tally[dc] = &folderTally{}
			}
			tally[dc].add(r)
		}
		var out []row
		for _, dc := range folderDatacenters(all) {
			out = append(out, m.folderRow(dc, humanize.Dash(dc.datacenter), tally[dc], true))
		}
		return out
	}

	prefix := spot.path
	if prefix != "/" {
		prefix += "/"
	}
	var leaves []row
	tally := map[string]*folderTally{}
	for _, r := range all {
		if r.context != spot.context || r.where.Datacenter != spot.datacenter {
			continue
		}
		f := rowFolder(r)
		if f == spot.path {
			leaves = append(leaves, r)
			continue
		}
		rest, ok := strings.CutPrefix(f, prefix)
		if !ok || rest == "" {
			continue
		}
		name, _, _ := strings.Cut(rest, "/")
		if tally[name] == nil {
			tally[name] = &folderTally{}
		}
		tally[name].add(r)
	}
	names := make([]string, 0, len(tally))
	for n := range tally {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	out := make([]row, 0, len(names)+len(leaves))
	for _, n := range names {
		child := spot
		child.path = prefix + n
		out = append(out, m.folderRow(child, n, tally[n], len(leaves) == 0))
	}
	m.sortMode.apply(leaves)
	return append(out, leaves...)
}

// folderTally is what a folder holds, at any depth.
type folderTally struct {
	n, on  int
	cpu    int64
	memMB  int64
	diskGB float64
}

func (t *folderTally) add(r row) {
	t.n++
	v := r.filed
	if v == nil {
		return
	}
	if v.PowerState == "poweredOn" {
		t.on++
	}
	t.cpu += int64(v.CPU)
	t.memMB += v.MemoryMB
	t.diskGB += v.StorageGB
}

// folderColumns are the table's columns on a level that holds only folders,
// where the VM or template columns would all be empty.
func folderColumns(kind vsphere.Kind, withContext bool) []column {
	var cols []column
	if withContext {
		cols = append(cols, column{title: "VCENTER", width: 14})
	}
	cols = append(cols, column{title: "NAME"})
	if kind == vsphere.KindTemplate {
		return append(cols,
			column{title: "TEMPLATES", width: 9, right: true},
			column{title: "vCPU", width: 6, right: true},
			column{title: "MEMORY", width: 8, right: true},
			column{title: "DISK", width: 8, right: true},
		)
	}
	return append(cols,
		column{title: "VMS", width: 6, right: true},
		column{title: "POWERED ON", width: 10, right: true},
		column{title: "vCPU", width: 6, right: true},
		column{title: "MEMORY", width: 8, right: true},
	)
}

// foldersOnly reports whether the level on show holds folders and nothing
// else, which is when the table takes folderColumns.
func foldersOnly(rows []row) bool {
	for _, r := range rows {
		if r.folder == nil {
			return false
		}
	}
	return len(rows) > 0
}

// folderRow is the line for a folder. Alone on its level it fills
// folderColumns; beside VMs or templates it fills only the name column,
// with its count, and leaves the rest of the line to their columns.
func (m *Model) folderRow(s folderSpot, label string, t *folderTally, alone bool) row {
	withContext := m.showContext()
	var cells []string
	if withContext {
		cells = append(cells, s.context)
	}
	switch {
	case alone && m.kind == vsphere.KindTemplate:
		cells = append(cells, label+"/", strconv.Itoa(t.n), strconv.FormatInt(t.cpu, 10),
			humanize.MB(t.memMB), humanize.GB(t.diskGB))
	case alone:
		cells = append(cells, label+"/", strconv.Itoa(t.n), strconv.Itoa(t.on),
			strconv.FormatInt(t.cpu, 10), humanize.MB(t.memMB))
	default:
		cells = append(cells, label+"/  "+kindCount(m.kind, t.n))
		for len(cells) < len(columnsFor(m.kind, withContext)) {
			cells = append(cells, "")
		}
	}
	spot := s
	return row{
		key:     "folder\x00" + s.context + "\x00" + s.datacenter + "\x00" + s.path,
		context: s.context,
		kind:    m.kind,
		name:    label,
		glyph:   glyphFolder,
		status:  statusIdle,
		where:   vsphere.Location{Context: s.context, Datacenter: s.datacenter},
		cells:   cells,
		folder:  &spot,
	}
}

// openFolder steps into the folder under the cursor.
func (m *Model) openFolder(s folderSpot) {
	m.setFolder(s)
	m.cursor, m.offset = 0, 0
}

// folderUp steps out to the parent folder and leaves the cursor on the
// folder it came from. It reports false at the top, where esc has nothing
// left to undo.
func (m *Model) folderUp() bool {
	all := m.folderSource()
	spot := m.currentFolder(all)
	if spot.root() {
		return false
	}
	parent := spot
	switch {
	case spot.path != "/":
		parent.path = spot.path[:strings.LastIndex(spot.path, "/")]
		if parent.path == "" {
			parent.path = "/"
		}
	case len(folderDatacenters(all)) > 1:
		parent = folderSpot{}
	default:
		return false
	}
	m.setFolder(parent)
	m.cursor, m.offset = 0, 0
	for i, r := range m.rows() {
		if r.folder != nil && *r.folder == spot {
			m.moveTo(i)
			break
		}
	}
	return true
}

// folderCrumb names the current folder for the message line:
// "DC0 › Prod › Web", led by the vCenter when several are in view.
func (m *Model) folderCrumb() string {
	spot := m.currentFolder(m.folderSource())
	if spot.root() {
		return "all datacenters"
	}
	parts := []string{humanize.Dash(spot.datacenter)}
	if m.showContext() {
		parts = append([]string{spot.context}, parts...)
	}
	for _, p := range strings.Split(strings.Trim(spot.path, "/"), "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " › ")
}

// folderAtTop reports whether esc has no folder left to climb out of.
func (m *Model) folderAtTop() bool {
	all := m.folderSource()
	spot := m.currentFolder(all)
	return spot.root() || (spot.path == "/" && len(folderDatacenters(all)) <= 1)
}

// stepPastFolders moves the detail pane's selection by delta and on past any
// folder rows, which have no pane of their own. With nothing but folders that
// way, the selection stays where it was.
func (m *Model) stepPastFolders(delta int) {
	from := m.cursor
	m.move(delta)
	for {
		r, ok := m.currentRow()
		if !ok || r.folder == nil {
			return
		}
		before := m.cursor
		m.move(delta)
		if m.cursor == before {
			m.moveTo(from)
			return
		}
	}
}
