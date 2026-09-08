package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vsfleet/internal/humanize"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// dsWorkspace is the read-only datastore file browser while it is open.
//
// It holds no tree. One directory is in memory at a time, because the estate
// this program is pointed at has datastores whose full contents nobody wants
// enumerated, let alone retained: navigation costs exactly one query per
// directory an operator opens, and walking back up re-reads rather than
// remembering. Recursive search is a separate, explicitly requested operation
// that lives in the same struct only so Esc can cancel either one.
type dsWorkspace struct {
	// context, datastore and datastoreID identify what is being browsed. The
	// moref value is carried because the browser is resolved from it on every
	// query — the inventory type does not keep one.
	context     string
	datastore   string
	datastoreID string

	// path is the directory being shown, relative to the datastore root.
	// Empty is the root.
	path      string
	entries   []vsphere.DatastoreEntry
	cursor    int
	offset    int
	loading   bool
	err       error
	truncated bool

	// find is the recursive search: its prompt, its last answered query, and
	// its results. It is deliberately a separate cursor from the directory's,
	// so cancelling a search leaves the directory exactly as it was.
	find          textinput.Model
	query         string
	results       []vsphere.DatastoreEntry
	findCursor    int
	findOffset    int
	finding       bool
	findErr       error
	findTruncated bool
	// findPrompt is whether the prompt is still being typed, as opposed to
	// showing the results of what was typed.
	findPrompt bool

	// generation rises with every request. A reply carrying an older one is
	// dropped, which is what stops a fast Enter-Esc-Enter from painting the
	// directory the operator has already left.
	generation uint64
	// cancel abandons whatever is in flight. Cancelling is client-side by
	// design: the reply is dropped and the context released, and no cancel
	// call is ever made to the vCenter, so the read-only guarantee this
	// program makes stays a guarantee about pure reads.
	cancel context.CancelFunc
}

const (
	dsTypeWidth     = 6
	dsSizeWidth     = 10
	dsModifiedWidth = 16
	// dsFindPlaceholder names what the prompt wants. A pattern, not a
	// substring: it goes to the server as a match pattern, so "*.iso" finds
	// what "iso" would not.
	dsFindPlaceholder = "name or pattern, e.g. *.iso"
)

// datastoreBrowser returns the live-query seam, if this backend has one.
func (m *Model) datastoreBrowser() (datastoreBrowserBackend, bool) {
	b, ok := m.backend.(datastoreBrowserBackend)
	return b, ok
}

// browseFilesAction opens the file browser on a datastore.
//
// Like openAction it reports its own unavailability rather than disappearing:
// an operator who cannot browse should be told why, not left wondering where
// the action went.
func (m *Model) browseFilesAction(r row) action {
	const label = "Browse files"
	if _, ok := m.datastoreBrowser(); !ok {
		return action{label: label, disabled: "file browsing is unavailable in this build"}
	}
	if st := m.byName[r.context]; st == nil {
		return action{label: label, disabled: "vCenter not in scope"}
	}
	if reason := datastoreBrowseReason(r); reason != "" {
		return action{label: label, disabled: reason}
	}
	return action{label: label, detail: r.target.path, run: func(m *Model) tea.Cmd {
		return m.openDatastoreFiles(r, false)
	}}
}

// findFilesAction is the recursive search, offered from the datastore's own
// action list so an operator who already knows they are looking for something
// does not have to walk into the root first.
func (m *Model) findFilesAction(r row) action {
	const label = "Find in datastore"
	if _, ok := m.datastoreBrowser(); !ok {
		return action{label: label, disabled: "file browsing is unavailable in this build"}
	}
	if st := m.byName[r.context]; st == nil {
		return action{label: label, disabled: "vCenter not in scope"}
	}
	if reason := datastoreBrowseReason(r); reason != "" {
		return action{label: label, disabled: reason}
	}
	return action{label: label, run: func(m *Model) tea.Cmd {
		return m.openDatastoreFiles(r, true)
	}}
}

// datastoreBrowseReason is why this datastore cannot be browsed at all, empty
// when it can. An inaccessible datastore is reported here rather than as an
// empty directory: "no files" and "the host cannot see this datastore" are
// different answers and must not look alike.
func datastoreBrowseReason(r row) string {
	if r.target.moref == "" {
		return "no datastore reference — reload and try again"
	}
	if r.target.inaccessible {
		return "datastore is inaccessible"
	}
	return ""
}

// openDatastoreFiles enters the workspace at the datastore's root. findFirst
// opens straight onto the search prompt.
func (m *Model) openDatastoreFiles(r row, findFirst bool) tea.Cmd {
	find := textinput.New()
	find.Prompt = "> "
	find.Placeholder = dsFindPlaceholder
	find.CharLimit = 128
	m.ds = &dsWorkspace{
		context:     r.context,
		datastore:   r.name,
		datastoreID: r.target.moref,
		find:        find,
	}
	m.actions = nil
	m.setMessage("", false)
	if findFirst {
		m.mode = modeDatastoreFind
		m.ds.findPrompt = true
		return m.ds.find.Focus()
	}
	m.mode = modeDatastoreFiles
	return m.listDatastoreDir(m.ds.path)
}

// clearDSWorkspace closes the browser, abandoning anything in flight.
func (m *Model) clearDSWorkspace() {
	if m.ds == nil {
		return
	}
	m.ds.abandon()
	m.ds = nil
}

// abandon cancels the request in flight, if any, and makes sure its reply is
// discarded when it arrives anyway.
func (w *dsWorkspace) abandon() {
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.generation++
	w.loading, w.finding = false, false
}

func (m *Model) dsContext() (*contextState, bool) {
	if m.ds == nil {
		return nil, false
	}
	st, ok := m.byName[m.ds.context]
	return st, ok && st != nil
}

// listDatastoreDir asks for one directory and nothing else.
func (m *Model) listDatastoreDir(path string) tea.Cmd {
	backend, ok := m.datastoreBrowser()
	st, inScope := m.dsContext()
	if !ok || !inScope {
		m.ds.err = fmt.Errorf("file browsing is unavailable for %s", m.ds.context)
		return nil
	}
	m.ds.abandon()
	m.ds.path = path
	m.ds.entries, m.ds.err, m.ds.truncated = nil, nil, false
	m.ds.cursor, m.ds.offset = 0, 0
	m.ds.loading = true

	ctx, cancel := context.WithCancel(m.ctx)
	m.ds.cancel = cancel
	return tea.Batch(
		listDatastoreDirCmd(ctx, backend, st.cc, *m.ds),
		m.spin.Tick,
	)
}

// findInDatastore runs the one recursive operation the browser offers.
func (m *Model) findInDatastore(pattern string) tea.Cmd {
	backend, ok := m.datastoreBrowser()
	st, inScope := m.dsContext()
	if !ok || !inScope {
		m.ds.findErr = fmt.Errorf("file browsing is unavailable for %s", m.ds.context)
		return nil
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		m.ds.findErr = fmt.Errorf("type something to search for")
		return nil
	}
	m.ds.abandon()
	m.ds.query = pattern
	m.ds.results, m.ds.findErr, m.ds.findTruncated = nil, nil, false
	m.ds.findCursor, m.ds.findOffset = 0, 0
	m.ds.finding = true
	m.ds.findPrompt = false
	m.ds.find.Blur()

	ctx, cancel := context.WithCancel(m.ctx)
	m.ds.cancel = cancel
	return tea.Batch(
		findInDatastoreCmd(ctx, backend, st.cc, *m.ds, pattern),
		m.spin.Tick,
	)
}

// applyDSListing lands a directory reply, dropping one the operator has
// already navigated away from.
func (m *Model) applyDSListing(msg dsListingMsg) tea.Cmd {
	if m.ds == nil || msg.generation != m.ds.generation || msg.path != m.ds.path {
		return nil
	}
	m.ds.loading = false
	m.ds.cancel = nil
	m.ds.err = msg.err
	m.ds.entries = msg.listing.Entries
	m.ds.truncated = msg.listing.Truncated
	m.ds.cursor, m.ds.offset = 0, 0
	return nil
}

func (m *Model) applyDSFind(msg dsFindMsg) tea.Cmd {
	if m.ds == nil || msg.generation != m.ds.generation || msg.query != m.ds.query {
		return nil
	}
	m.ds.finding = false
	m.ds.cancel = nil
	m.ds.findErr = msg.err
	m.ds.results = msg.listing.Entries
	m.ds.findTruncated = msg.listing.Truncated
	m.ds.findCursor, m.ds.findOffset = 0, 0
	return nil
}

// visibleDSEntries applies the in-memory filter over the current directory.
// Filtering never re-queries: it narrows what was already read, which is why
// it is instant and why it cannot find anything in a directory not open.
func (m *Model) visibleDSEntries() []vsphere.DatastoreEntry {
	if m.ds == nil {
		return nil
	}
	needle := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if needle == "" || m.mode != modeDatastoreFiles {
		return m.ds.entries
	}
	out := make([]vsphere.DatastoreEntry, 0, len(m.ds.entries))
	for _, entry := range m.ds.entries {
		if strings.Contains(strings.ToLower(entry.Name), needle) {
			out = append(out, entry)
		}
	}
	return out
}

func (m *Model) handleDatastoreFilesKey(msg tea.KeyMsg) tea.Cmd {
	if m.ds == nil {
		m.mode = modeDetail
		return nil
	}
	if m.actions != nil {
		return m.handleActionsKey(msg)
	}
	entries := m.visibleDSEntries()
	page := max(1, m.bodyHeight()-6)
	switch {
	case key.Matches(msg, m.keys.Back):
		return m.leaveDatastoreDir()
	case key.Matches(msg, m.keys.Up):
		m.ds.cursor = clamp(m.ds.cursor-1, 0, max(0, len(entries)-1))
	case key.Matches(msg, m.keys.Down):
		m.ds.cursor = clamp(m.ds.cursor+1, 0, max(0, len(entries)-1))
	case key.Matches(msg, m.keys.PageUp):
		m.ds.cursor = clamp(m.ds.cursor-page, 0, max(0, len(entries)-1))
	case key.Matches(msg, m.keys.PageDown):
		m.ds.cursor = clamp(m.ds.cursor+page, 0, max(0, len(entries)-1))
	case key.Matches(msg, m.keys.Home):
		m.ds.cursor = 0
	case key.Matches(msg, m.keys.End):
		m.ds.cursor = max(0, len(entries)-1)
	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		m.filter.Placeholder = "filter this directory"
		return m.filter.Focus()
	case key.Matches(msg, m.keys.FindFiles):
		m.ds.findPrompt = true
		m.ds.find.SetValue("")
		m.ds.findErr = nil
		m.mode = modeDatastoreFind
		return m.ds.find.Focus()
	case key.Matches(msg, m.keys.CopyPath):
		if entry, ok := selectedEntry(entries, m.ds.cursor); ok {
			return m.copyCmd(entry.Path)
		}
	case key.Matches(msg, m.keys.Open):
		entry, ok := selectedEntry(entries, m.ds.cursor)
		if !ok {
			return nil
		}
		if entry.Type == vsphere.DatastoreEntryFolder {
			m.filter.SetValue("")
			return m.listDatastoreDir(vsphere.ChildBrowsePath(m.ds.path, entry.Name))
		}
		m.actions = &actionList{items: datastoreEntryActions(entry)}
	}
	return nil
}

// leaveDatastoreDir walks up one level, or leaves the workspace at the root.
func (m *Model) leaveDatastoreDir() tea.Cmd {
	// An Esc while a directory is still coming means "stop", not "go up": the
	// operator is waiting on a request they no longer want.
	if m.ds.loading {
		m.ds.abandon()
		m.ds.err = nil
		return nil
	}
	m.filter.SetValue("")
	m.filter.Placeholder = filterPlaceholder
	if m.ds.path == "" {
		m.clearDSWorkspace()
		m.mode = modeDetail
		return nil
	}
	return m.listDatastoreDir(vsphere.ParentBrowsePath(m.ds.path))
}

func (m *Model) handleDatastoreFindKey(msg tea.KeyMsg) tea.Cmd {
	if m.ds == nil {
		m.mode = modeDetail
		return nil
	}
	if m.ds.findPrompt {
		switch msg.Type {
		case tea.KeyEsc:
			m.ds.findPrompt = false
			m.ds.find.Blur()
			if m.ds.query == "" {
				m.mode = modeDatastoreFiles
				return m.listDatastoreDirIfEmpty()
			}
			return nil
		case tea.KeyEnter:
			return m.findInDatastore(m.ds.find.Value())
		}
		var cmd tea.Cmd
		m.ds.find, cmd = m.ds.find.Update(msg)
		return cmd
	}

	page := max(1, m.bodyHeight()-6)
	switch {
	case key.Matches(msg, m.keys.Back):
		// Cancelling a search in flight stops it rather than closing the
		// pane, so the operator sees that it stopped.
		if m.ds.finding {
			m.ds.abandon()
			m.ds.findErr = fmt.Errorf("search cancelled")
			return nil
		}
		m.mode = modeDatastoreFiles
		return m.listDatastoreDirIfEmpty()
	case key.Matches(msg, m.keys.Up):
		m.ds.findCursor = clamp(m.ds.findCursor-1, 0, max(0, len(m.ds.results)-1))
	case key.Matches(msg, m.keys.Down):
		m.ds.findCursor = clamp(m.ds.findCursor+1, 0, max(0, len(m.ds.results)-1))
	case key.Matches(msg, m.keys.PageUp):
		m.ds.findCursor = clamp(m.ds.findCursor-page, 0, max(0, len(m.ds.results)-1))
	case key.Matches(msg, m.keys.PageDown):
		m.ds.findCursor = clamp(m.ds.findCursor+page, 0, max(0, len(m.ds.results)-1))
	case key.Matches(msg, m.keys.Home):
		m.ds.findCursor = 0
	case key.Matches(msg, m.keys.End):
		m.ds.findCursor = max(0, len(m.ds.results)-1)
	case key.Matches(msg, m.keys.FindFiles):
		m.ds.findPrompt = true
		m.ds.findErr = nil
		return m.ds.find.Focus()
	case key.Matches(msg, m.keys.CopyPath):
		if entry, ok := selectedEntry(m.ds.results, m.ds.findCursor); ok {
			return m.copyCmd(entry.Path)
		}
	case key.Matches(msg, m.keys.Open):
		// Enter on a result is "show me where that is": it opens the
		// directory holding the match, which is the question the search was
		// asked in the first place.
		entry, ok := selectedEntry(m.ds.results, m.ds.findCursor)
		if !ok {
			return nil
		}
		m.mode = modeDatastoreFiles
		m.filter.SetValue("")
		return m.listDatastoreDir(datastoreEntryDir(m.ds.datastore, entry))
	}
	return nil
}

// listDatastoreDirIfEmpty re-reads the current directory when returning to a
// browser that never read one — entering through "Find in datastore" skips
// the root listing, so coming back from the results is where it happens.
func (m *Model) listDatastoreDirIfEmpty() tea.Cmd {
	if len(m.ds.entries) > 0 || m.ds.loading || m.ds.err != nil {
		return nil
	}
	return m.listDatastoreDir(m.ds.path)
}

func selectedEntry(entries []vsphere.DatastoreEntry, cursor int) (vsphere.DatastoreEntry, bool) {
	if cursor < 0 || cursor >= len(entries) {
		return vsphere.DatastoreEntry{}, false
	}
	return entries[cursor], true
}

// datastoreEntryDir is the directory containing an entry, relative to the
// datastore root.
func datastoreEntryDir(datastore string, entry vsphere.DatastoreEntry) string {
	_, relative, ok := vsphere.SplitBrowsePath(entry.Path)
	if !ok {
		return ""
	}
	if entry.Type == vsphere.DatastoreEntryFolder {
		return relative
	}
	return vsphere.ParentBrowsePath(relative)
}

// datastoreEntryActions is what a file offers. The list is short on purpose:
// this workspace inspects, and every action that would change something on a
// datastore is absent because it was never written.
func datastoreEntryActions(entry vsphere.DatastoreEntry) []action {
	return []action{
		copyNamed("Copy datastore path", entry.Path),
		copyNamed("Copy file name", entry.Name),
	}
}

func dsColumns() []column {
	return []column{
		{title: "TYPE", width: dsTypeWidth},
		{title: "NAME"},
		{title: "SIZE", width: dsSizeWidth, right: true},
		{title: "MODIFIED", width: dsModifiedWidth},
	}
}

func (m *Model) viewDatastoreFiles() []string {
	if m.ds == nil {
		return []string{m.theme.dim.Render("nothing selected")}
	}
	t := m.theme
	lines := []string{joinEnds(
		t.title.Render(m.ds.datastore)+t.dim.Render("   datastore · "+m.ds.context),
		t.dim.Render("read-only"),
		m.width,
	), ""}
	lines = append(lines, truncate("  "+t.label.Render("Path")+t.value.Render("  "+m.dsBreadcrumb()), m.width), "")

	switch {
	case m.ds.loading:
		return append(lines, "  "+m.spin.View()+t.dim.Render(" listing "+m.dsBreadcrumb()+"…"))
	case m.ds.err != nil:
		// A failure is shown as a failure. Rendering a denied or unreachable
		// directory as an empty one would answer a question nobody asked.
		return append(lines,
			"  "+t.warn.Render(m.ds.err.Error()),
			"  "+t.dim.Render("esc goes back · r is not a retry here, esc and re-enter"))
	}

	entries := m.visibleDSEntries()
	m.ds.cursor = clamp(m.ds.cursor, 0, max(0, len(entries)-1))
	lines = append(lines, m.dsTable(entries, m.ds.cursor, &m.ds.offset, "empty directory", false)...)
	if m.ds.truncated {
		lines = append(lines, "  "+t.warn.Render(fmt.Sprintf("showing the first %d entries — this directory is larger than that", len(m.ds.entries))))
	}
	return scrollLines(lines, 0, m.bodyHeight())
}

func (m *Model) viewDatastoreFind() []string {
	if m.ds == nil {
		return []string{m.theme.dim.Render("nothing selected")}
	}
	t := m.theme
	lines := []string{joinEnds(
		t.title.Render("Find in "+m.ds.datastore)+t.dim.Render("   datastore · "+m.ds.context),
		t.dim.Render("recursive · read-only"),
		m.width,
	), ""}
	lines = append(lines, "  "+m.ds.find.View(), "")

	switch {
	case m.ds.findPrompt:
		return append(lines, "  "+t.dim.Render("enter searches every directory · esc goes back"))
	case m.ds.finding:
		return append(lines, "  "+m.spin.View()+t.dim.Render(" searching "+m.ds.datastore+" for "+m.ds.query+"… esc cancels"))
	case m.ds.findErr != nil:
		return append(lines, "  "+t.warn.Render(m.ds.findErr.Error()))
	}

	m.ds.findCursor = clamp(m.ds.findCursor, 0, max(0, len(m.ds.results)-1))
	lines = append(lines, m.dsTable(m.ds.results, m.ds.findCursor, &m.ds.findOffset, "nothing matched "+m.ds.query, true)...)
	if m.ds.findTruncated {
		// Truncation stays on screen beside the results rather than passing
		// by as a message. A partial search that reads as a complete one is
		// how an operator concludes a file is not there when it is.
		lines = append(lines, "  "+t.warn.Render(fmt.Sprintf("partial result — stopped at %d matches; narrow the pattern", len(m.ds.results))))
	}
	return scrollLines(lines, 0, m.bodyHeight())
}

// dsTable renders one list of entries with its own scroll window. showPath
// switches the NAME column to the full datastore path, which is what search
// results are for: a leaf name repeated down the screen answers nothing, and
// where the match is was the question.
func (m *Model) dsTable(entries []vsphere.DatastoreEntry, cursor int, offset *int, empty string, showPath bool) []string {
	t := m.theme
	cols := dsColumns()
	widths := layoutColumns(cols, m.width-glyphGutter)
	head := make([]string, 0, len(cols))
	for i, c := range cols {
		if widths[i] > 0 {
			head = append(head, pad(c.title, widths[i], c.right))
		}
	}
	lines := []string{t.header.Render(strings.Repeat(" ", glyphGutter) + strings.Join(head, strings.Repeat(" ", cellGap)))}
	if len(entries) == 0 {
		return append(lines, "  "+t.dim.Render(empty))
	}

	height := max(1, m.bodyHeight()-len(lines)-6)
	if cursor < *offset {
		*offset = cursor
	}
	if cursor >= *offset+height {
		*offset = cursor - height + 1
	}
	*offset = clamp(*offset, 0, max(0, len(entries)-height))
	for i := *offset; i < len(entries) && i < *offset+height; i++ {
		lines = append(lines, m.renderDSEntry(entries[i], cols, widths, i == cursor, showPath))
	}
	return lines
}

func (m *Model) renderDSEntry(entry vsphere.DatastoreEntry, cols []column, widths []int, selected, showPath bool) string {
	kind, glyph, name := "FILE", " ", entry.Name
	size := humanize.Bytes(entry.SizeBytes)
	if entry.Type == vsphere.DatastoreEntryFolder {
		kind, glyph, name, size = "DIR", "▸", entry.Name+"/", "—"
	}
	if showPath {
		if _, relative, ok := vsphere.SplitBrowsePath(entry.Path); ok && relative != "" {
			name = relative
			if entry.Type == vsphere.DatastoreEntryFolder {
				name += "/"
			}
		}
	}
	modified := "—"
	if !entry.Modified.IsZero() {
		modified = entry.Modified.Format("2006-01-02 15:04")
	}
	cells := []string{kind, name, size, modified}
	drawn := make([]string, 0, len(cols))
	for i, c := range cols {
		if widths[i] == 0 {
			continue
		}
		drawn = append(drawn, pad(cells[i], widths[i], c.right))
	}
	line := strings.Join(drawn, strings.Repeat(" ", cellGap))
	if selected {
		line = m.theme.focused.Render(line)
	} else {
		line = m.theme.text.Render(line)
	}
	return m.theme.accent.Render(glyph) + " " + line
}

// dsBreadcrumb is where the operator is, said plainly.
func (m *Model) dsBreadcrumb() string {
	if m.ds == nil || m.ds.path == "" {
		return "/"
	}
	return "/" + m.ds.path
}
