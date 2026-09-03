package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vsfleet/internal/cache"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/session"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// maxConcurrentLoads bounds how many contexts fetch their inventory at once.
// Without a bound, an estate with dozens of contexts would open that many
// connections the instant the interface starts.
const maxConcurrentLoads = 4

// DefaultRefreshInterval is how often the inventory on screen is re-read.
//
// Power state, IP addresses and usage all move under a table left open, and
// nothing else would ever correct them. Twenty seconds is short enough that
// a change made elsewhere shows up before anyone reaches for the reload key,
// which is the number that matters: an interval slower than an operator's
// patience gets overridden by hand, and then it may as well not exist.
//
// It is affordable because it applies to the vCenter being looked at, not to
// every configured one — see refreshDue. Operators who disagree in either
// direction can say so with --refresh, including a negative value to read
// only when asked.
const DefaultRefreshInterval = 20 * time.Second

// backgroundRefreshFactor is how much slower a vCenter nobody is looking at
// is re-read. A full read costs roughly 2.7 KiB per inventory object, so
// holding an entire estate to the on-screen interval would multiply that by
// the number of contexts configured — continuously, to keep current a set of
// numbers not on screen. What off-screen freshness actually serves is the
// header count and estate-wide search, neither of which changes meaning over
// a few minutes.
const backgroundRefreshFactor = 10

// mode is which full-screen view is showing. Detail, diagnosis and help all
// replace the table rather than floating over it: a half-covered table invites
// you to read a number that is no longer the number you are looking at.
type mode int

const (
	modeBrowse mode = iota
	modeDetail
	modeDoctor
	modeHelp
	modeForm
	modeConfirmDelete
	modeContexts
	modeSearch
)

// searchState is one estate-wide search: every kind, every vCenter, matched
// on name the same way "vsfleet search" matches on the command line.
//
// It is answered from the inventories already in memory rather than by going
// back to the vCenters. Init prefetches every context, so by the time anyone
// asks, the answer is on hand — and a vCenter that never answered is reported
// as not searched rather than quietly narrowing the result.
type searchState struct {
	query    string
	rows     []row
	searched int
	missing  []*contextState
}

// contextState is one vCenter as the UI sees it: the configuration, the last
// inventory fetched from it, and whatever went wrong instead. A failed context
// keeps its row — that a customer environment is unreachable is information,
// not a reason to hide it.
type contextState struct {
	cc       *config.Context
	inv      *vsphere.Inventory
	err      error
	loading  bool
	elapsed  time.Duration
	loadedAt time.Time
	diag     *vsphere.Diagnosis
	diagging bool
	// quiet marks the in-flight read as one nobody asked for — the periodic
	// background refresh rather than a keystroke. It suppresses the pending
	// indicator and the success message, so a table being kept current does
	// not flicker through "connecting…" once a minute. A quiet read that
	// fails is not quiet: that is the moment the reader needs telling.
	quiet bool
}

// reset drops everything this state learned from a vCenter, keeping only the
// configuration. It is what an edited context needs: the name is the same, so
// the row stays, but every inventory, error and diagnosis behind it came from
// a server this context no longer points at.
//
// loading is deliberately left alone. A fetch already in flight still holds
// the flag that stops a second one starting, and its result is discarded on
// arrival by the context it was issued for rather than by clearing a flag that
// would then be wrong.
func (s *contextState) reset() {
	s.inv = nil
	s.err = nil
	s.elapsed = 0
	s.loadedAt = time.Time{}
	s.diag = nil
}

// showsLoading is whether a fetch in flight should be advertised. The loading
// flag itself stays true for a background refresh — it is what stops a second
// read starting — but nothing on screen changes for it.
func (s *contextState) showsLoading() bool { return s.loading && !s.quiet }

// A context can be both erroring and holding data at once — the cache keeps
// the last inventory that loaded successfully even when the most recent
// refresh failed, so that case gets its own status between "never connected"
// (bad) and "current" (good) rather than losing the stale data's presence.
func (s *contextState) rowStatus() rowStatus {
	switch {
	case s.showsLoading() || s.diagging:
		return statusWarn
	case s.err != nil && s.inv == nil:
		return statusBad
	case s.err != nil:
		return statusWarn
	case s.inv != nil:
		return statusGood
	default:
		return statusIdle
	}
}

func (s *contextState) glyph() string {
	switch {
	case s.showsLoading() || s.diagging:
		return glyphPending
	case s.err != nil && s.inv == nil:
		return glyphFail
	case s.err != nil:
		return glyphOnline
	case s.inv != nil:
		return glyphOnline
	default:
		return glyphOffline
	}
}

// Options configure the interface at start-up.
type Options struct {
	// Current is the context the cursor starts on. Empty starts at the first.
	Current string
	// AllContexts starts with every vCenter in view rather than one.
	AllContexts bool
	// Kind is the resource tab to start on. An unrecognised or empty value
	// starts on VMs, the same as a first run.
	Kind string
	// Sort is the sort mode to start in: "status", or anything else
	// (including empty) for the default name order.
	Sort string
	// RefreshInterval is how often inventory is re-read in the background.
	// Zero means DefaultRefreshInterval; negative means never, leaving the
	// table exactly as last read until someone asks for more.
	RefreshInterval time.Duration
}

// Snapshot is what is worth remembering about the interface between runs:
// where the cursor was, not the inventory or connection state, which a new
// run fetches fresh anyway.
type Snapshot struct {
	Context string
	Kind    string
	Sort    string
}

// Snapshot reports the interface's current position, for the caller to
// persist once the program exits.
func (m *Model) Snapshot() Snapshot {
	snap := Snapshot{Kind: string(m.kind), Sort: m.sortMode.label()}
	if st := m.current(); st != nil {
		snap.Context = st.cc.Name
	}
	return snap
}

// Model is the whole interface. Every field is presentation state: nothing
// here is the only copy of anything, so the interface can be rewritten without
// consulting the rest of the program.
type Model struct {
	ctx     context.Context
	backend Backend
	cache   *cache.Cache
	keys    keyMap
	theme   theme
	spin    spinner.Model
	filter  textinput.Model

	states []*contextState
	byName map[string]*contextState

	selected  int
	kind      vsphere.Kind
	sortMode  sortMode
	allScope  bool
	filtering bool
	mode      mode

	cursor  int
	offset  int
	detailY int

	// ctxCursor is the row the contexts screen is on, which is only the same
	// as selected until you start moving around in there without choosing
	// anything.
	ctxCursor int
	// returnTo is the screen the form or the delete confirmation was opened
	// from, so cancelling lands back where you were rather than on the table.
	returnTo mode
	// search holds the last estate-wide result set, and searchDirty marks it
	// stale after an inventory arrives or the context list changes.
	search      *searchState
	searchDirty bool
	// detailFrom is the screen the detail pane was opened from, so esc goes
	// back to the search results rather than always to the table.
	detailFrom mode
	// doctor is the context the diagnosis panel is reporting on. It is not
	// always the one in scope: in all-vCenters view "d" asks about the
	// vCenter the selected row came from.
	doctor *contextState

	// form holds the add/edit context form while mode is modeForm.
	form *contextForm
	// confirmDelete is the context pending removal while mode is
	// modeConfirmDelete, and confirmAlsoCredential is whether its stored
	// password is removed along with it.
	confirmDelete         *contextState
	confirmAlsoCredential bool

	// refreshInterval is how often inventory is re-read without being asked.
	// Zero disables it entirely.
	refreshInterval time.Duration

	width, height int
	message       string
	messageBad    bool
	quitting      bool
}

// New builds the interface over a backend.
func New(ctx context.Context, backend Backend, opts Options) *Model {
	contexts := backend.Contexts()
	kind := vsphere.KindVM
	if k, err := vsphere.ParseKind(opts.Kind); err == nil {
		kind = k
	}
	sm := sortByName
	if opts.Sort == "status" {
		sm = sortByStatus
	}
	m := &Model{
		ctx:      ctx,
		backend:  backend,
		cache:    cache.New(maxConcurrentLoads),
		keys:     defaultKeys(),
		theme:    newTheme(),
		spin:     spinner.New(spinner.WithSpinner(spinner.Dot)),
		filter:   newFilterInput(),
		byName:   make(map[string]*contextState, len(contexts)),
		kind:     kind,
		sortMode: sm,
		allScope: opts.AllContexts,
		width:    100,
		height:   30,

		refreshInterval: refreshInterval(opts.RefreshInterval),
	}
	for i, cc := range contexts {
		st := &contextState{cc: cc}
		m.states = append(m.states, st)
		m.byName[cc.Name] = st
		if cc.Name == opts.Current {
			m.selected = i
		}
	}
	return m
}

// filterPlaceholder is what the query line offers when the filter is
// narrowing the table; the search screen replaces it with its own.
const filterPlaceholder = "filter by name"

func newFilterInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = filterPlaceholder
	ti.CharLimit = 128
	return ti
}

// refreshInterval resolves the configured interval: zero takes the default,
// negative means never. Only New calls it, so the model's own field is
// afterwards the single answer to "is background refresh on".
func refreshInterval(d time.Duration) time.Duration {
	switch {
	case d == 0:
		return DefaultRefreshInterval
	case d < 0:
		return 0
	default:
		return d
	}
}

// Init starts the spinner, prefetches every configured context, selected one
// first, and arms the background refresh. With no contexts configured yet
// there is nothing to load, so it opens the setup form instead of an empty
// table with no way to fill it.
func (m *Model) Init() tea.Cmd {
	if len(m.states) == 0 {
		return tea.Batch(m.enterForm(nil), m.spin.Tick)
	}
	cmds := m.prefetch()
	if cmd := scheduleRefresh(m.refreshInterval); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// inScope returns the contexts currently being displayed.
func (m *Model) inScope() []*contextState {
	if m.allScope {
		return m.states
	}
	if len(m.states) == 0 {
		return nil
	}
	return m.states[m.selected : m.selected+1]
}

// showContext reports whether rows need to say which vCenter they came from.
func (m *Model) showContext() bool { return m.allScope && len(m.states) > 1 }

// startLoad begins loading one context if it is not already loading and,
// unless force is set, does not already have a result — success, failure or
// stale-but-cached, all count. It returns nil when there is nothing to do.
func (m *Model) startLoad(st *contextState, force bool) tea.Cmd {
	return m.beginLoad(st, force, false)
}

// beginLoad is the shared body of every read. quiet marks it as one nobody
// asked for, which changes nothing about the fetch and everything about how
// it is reported: see contextState.quiet.
func (m *Model) beginLoad(st *contextState, force, quiet bool) tea.Cmd {
	if st.loading {
		return nil
	}
	if !force && (st.inv != nil || st.err != nil) {
		return nil
	}
	st.loading = true
	st.quiet = quiet
	return loadInventory(m.ctx, m.cache, m.backend, st.cc)
}

// refreshDue reports whether a context is old enough to be worth re-reading.
//
// What is on screen is held to the configured interval; everything else to
// backgroundRefreshFactor times it. The split is the whole point: a full
// re-read costs roughly 2.7 KiB per inventory object, so re-reading an
// estate at the rate a person wants for the table in front of them scales
// that cost by the number of vCenters configured, to keep current a set of
// numbers nobody is looking at. Off screen, what still has to be roughly
// right is the header count and an estate-wide search — neither of which
// changes meaning over a few minutes.
//
// The on-screen threshold is half the interval rather than the whole of it.
// Ticks land roughly one interval after the last read, but only roughly: a
// read that took two seconds leaves the next tick arriving just under the
// interval, and comparing against the whole of it would skip that cycle and
// wait for another. Half clears that jitter and still leaves a manual reload
// from a moment ago alone. Off screen the threshold is many ticks long, so
// there is no jitter to clear and it is compared exactly.
//
// A context that has never loaded — including one whose every attempt has
// failed — is always due, which is what lets a vCenter that was unreachable
// at start-up come back on its own rather than waiting for a keystroke.
func (m *Model) refreshDue(st *contextState, onScreen bool) bool {
	if st.loadedAt.IsZero() {
		return true
	}
	age := time.Since(st.loadedAt)
	if onScreen {
		return age >= m.refreshInterval/2
	}
	return age >= m.refreshInterval*backgroundRefreshFactor
}

// refreshStale re-reads every context due for it, quietly. It covers all of
// them rather than only what is on screen — the header summary and an
// estate-wide search both answer from contexts not currently in view — but
// at two different rates: see refreshDue.
//
// The all-vCenters view puts every context on screen, and there they are all
// held to the fast interval. That is the reader asking to watch the whole
// estate at once, and the cache's own concurrency bound is what keeps the
// answer from arriving as one burst.
//
// Nothing here forces a read past one already in flight, so a vCenter slower
// than the interval simply refreshes less often instead of queueing work
// behind itself.
func (m *Model) refreshStale() []tea.Cmd {
	if m.refreshInterval <= 0 {
		return nil
	}
	onScreen := make(map[*contextState]bool, len(m.states))
	for _, st := range m.inScope() {
		onScreen[st] = true
	}
	var cmds []tea.Cmd
	for _, st := range m.states {
		if !m.refreshDue(st, onScreen[st]) {
			continue
		}
		if cmd := m.beginLoad(st, true, true); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

// ensureLoaded returns load commands for everything in scope. force reloads
// contexts that already have an inventory.
func (m *Model) ensureLoaded(force bool) []tea.Cmd {
	var cmds []tea.Cmd
	for _, st := range m.inScope() {
		if cmd := m.startLoad(st, force); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) > 0 {
		cmds = append(cmds, m.spin.Tick)
	}
	return cmds
}

// enterScope is what every change of what is on screen runs. It loads
// anything never read, and quietly re-reads anything that was being held to
// the slower off-screen rate and is now being looked at — so arriving at a
// vCenter shows its current state rather than whatever it looked like up to
// several minutes ago, without waiting for the next tick.
func (m *Model) enterScope() tea.Cmd {
	cmds := m.ensureLoaded(false)
	// refreshStale skips anything ensureLoaded just started, so a context
	// cannot be read twice for one keystroke.
	cmds = append(cmds, m.refreshStale()...)
	return tea.Batch(cmds...)
}

// prefetch starts a background load for every configured context, not only
// what is currently in scope, so switching to a context not yet visited
// shows cached data immediately instead of a fresh spinner. The selected
// context's command is issued first, giving it the earliest claim on the
// cache's bounded concurrency — not a hard guarantee of which finishes
// first, but enough of a head start that it is the one most likely to.
func (m *Model) prefetch() []tea.Cmd {
	var cmds []tea.Cmd
	cur := m.current()
	if cur != nil {
		if cmd := m.startLoad(cur, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	for _, st := range m.states {
		if st == cur {
			continue
		}
		if cmd := m.startLoad(st, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) > 0 {
		cmds = append(cmds, m.spin.Tick)
	}
	return cmds
}

// visibleRows is the list the cursor is moving through: the estate-wide
// search results when a search is open, the current kind's table otherwise.
func (m *Model) visibleRows() []row {
	// A detail pane is a view onto the list it was opened from, so it keeps
	// reading that one: opened from a search result, moving on with ←/→ walks
	// the search results, not whichever tab is sitting behind them.
	mode := m.mode
	if mode == modeDetail {
		mode = m.detailFrom
	}
	if mode == modeSearch {
		return m.ensureSearch(m.filter.Value()).rows
	}
	return m.rows()
}

// ensureSearch answers a query from the loaded inventories, reusing the last
// result when neither the query nor the inventories have changed — which is
// what lets the browse screen show a live "and this many in the whole estate"
// count while you are still typing.
func (m *Model) ensureSearch(query string) *searchState {
	q := strings.ToLower(strings.TrimSpace(query))
	if m.search != nil && m.search.query == q && !m.searchDirty {
		return m.search
	}
	st := &searchState{query: q}
	for _, cs := range m.states {
		if cs.inv == nil {
			st.missing = append(st.missing, cs)
			continue
		}
		st.searched++
		if q == "" {
			continue
		}
		for _, k := range vsphere.AllKinds {
			for _, r := range rowsFor(cs.inv, k, false) {
				if strings.Contains(strings.ToLower(r.name), q) {
					st.rows = append(st.rows, r)
				}
			}
		}
	}
	m.sortMode.apply(st.rows)
	m.search, m.searchDirty = st, false
	return st
}

// rows builds the table for the active tab, across everything in scope.
func (m *Model) rows() []row {
	var out []row
	for _, st := range m.inScope() {
		out = append(out, rowsFor(st.inv, m.kind, m.showContext())...)
	}
	needle := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if needle != "" {
		kept := out[:0]
		for _, r := range out {
			if strings.Contains(strings.ToLower(r.name), needle) {
				kept = append(kept, r)
			}
		}
		out = kept
	}
	m.sortMode.apply(out)
	return out
}

// failuresInScope lists the contexts with no usable data at all, so the
// browse view can say so without the reader having to notice a missing row.
// A context that loaded once and is merely stale — the last refresh failed
// but earlier data is still on hand — belongs in the table above, with its
// sidebar glyph carrying the warning, not in this banner: showing both a row
// of real (if aging) data and a "this context is broken" notice at once
// would tell the reader two contradictory things.
func (m *Model) failuresInScope() []*contextState {
	var out []*contextState
	for _, st := range m.inScope() {
		if st.err != nil && st.inv == nil {
			out = append(out, st)
		}
	}
	return out
}

// kindErrorInScope reports whether any context in scope failed to list kind —
// a connected context missing one privilege, not a context that never
// connected at all — so the tab bar can flag it without the reader having to
// open every context's detail to notice.
func (m *Model) kindErrorInScope(kind vsphere.Kind) bool {
	for _, st := range m.inScope() {
		if st.inv == nil {
			continue
		}
		if _, ok := st.inv.ErrorFor(kind); ok {
			return true
		}
	}
	return false
}

func (m *Model) current() *contextState {
	if len(m.states) == 0 {
		return nil
	}
	if m.selected < 0 || m.selected >= len(m.states) {
		return nil
	}
	return m.states[m.selected]
}

// currentRow returns the row under the cursor, if any.
func (m *Model) currentRow() (row, bool) {
	rows := m.visibleRows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return row{}, false
	}
	return rows[m.cursor], true
}

// counts totals one kind across everything in scope, for the tab bar.
func (m *Model) count(kind vsphere.Kind) int {
	n := 0
	for _, st := range m.inScope() {
		n += countFor(st.inv, kind)
	}
	return n
}

// status returns the connection status of a context, falling back to a bare
// snapshot when nothing has been attempted.
func (m *Model) status(name string) session.Status {
	st, _ := m.backend.Status(name)
	return st
}

// Update is the whole event loop.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampCursor()
		return m, nil

	case spinner.TickMsg:
		if !m.busy() {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case inventoryMsg:
		return m, m.applyInventory(msg)

	case refreshTickMsg:
		// The next tick is armed whatever happens here, including when this
		// one refreshes nothing: a paused cycle must not be a stopped one.
		cmds := m.refreshStale()
		if cmd := scheduleRefresh(m.refreshInterval); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case diagnosisMsg:
		if st, ok := m.byName[msg.context]; ok {
			st.diagging = false
			// A diagnosis of the endpoint this context had when the walk
			// started explains nothing about the one it has now.
			if msg.cc == nil || st.cc == msg.cc {
				st.diag = msg.diagnosis
			}
		}
		return m, nil

	case formTestMsg:
		if m.form != nil {
			m.form.testing = false
			m.form.diag = msg.diagnosis
			if msg.diagnosis != nil && msg.diagnosis.OK() {
				m.form.note = "Connected to " + msg.diagnosis.About.FullVersion()
			}
		}
		return m, nil

	case formDiscoverMsg:
		if m.form != nil {
			m.form.discovering = false
			if msg.err != nil {
				m.form.err = msg.err.Error()
			} else {
				m.form.thumbprint.SetValue(msg.sha256)
				m.form.note = "Discovered " + msg.subject + ", expires " + msg.notAfter.Format("2006-01-02")
			}
		}
		return m, nil

	case formSaveMsg:
		return m, m.applyFormSave(msg)

	case formDeleteMsg:
		return m, m.applyFormDelete(msg)

	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

// applyFormSave lands the outcome of a save. On success the form closes, the
// context list is rebuilt from the backend, and the new or edited context is
// selected and loaded. On failure the form stays open with the diagnosis and
// error visible, so nothing typed is lost.
func (m *Model) applyFormSave(msg formSaveMsg) tea.Cmd {
	if m.form == nil {
		return nil
	}
	m.form.saving = false
	if msg.err != nil {
		m.form.err = msg.err.Error()
		if msg.result != nil {
			m.form.diag = msg.result.Diagnosis
			if msg.result.Diagnosis != nil && !msg.result.Diagnosis.OK() {
				m.form.forceSave = true
			}
		}
		return nil
	}
	name := msg.result.Context.Name
	m.syncContexts()
	m.selectByName(name)
	m.form = nil
	m.leaveOverlay()
	note := "saved context " + name
	if msg.result.StoreWarning != nil {
		note += " (password not stored: " + msg.result.StoreWarning.Error() + ")"
	}
	m.setMessage(note, msg.result.StoreWarning != nil)
	return tea.Batch(m.reload(false)...)
}

// applyFormDelete lands the outcome of removing a context. Removing the last
// one reopens the setup form: an empty screen with no way back in is not a
// resting state.
func (m *Model) applyFormDelete(msg formDeleteMsg) tea.Cmd {
	if msg.err != nil {
		m.setMessage(msg.err.Error(), true)
	} else {
		m.setMessage("removed context "+msg.name, false)
	}
	m.confirmDelete = nil
	m.syncContexts()
	if len(m.states) == 0 {
		return m.enterForm(nil)
	}
	m.leaveOverlay()
	return nil
}

// leaveOverlay closes the form or the delete confirmation and returns to the
// screen that opened it — the contexts list when you got there through "c",
// the table otherwise. Cancelling should never teleport you somewhere you
// were not.
func (m *Model) leaveOverlay() {
	m.mode = m.returnTo
	m.returnTo = modeBrowse
	m.selected = clamp(m.selected, 0, max(0, len(m.states)-1))
	m.ctxCursor = clamp(m.ctxCursor, 0, max(0, len(m.states)-1))
}

// syncContexts rebuilds the context list from the backend after a save or a
// removal, keeping the loaded inventory of every context that still exists and
// still points where it did. A context whose name survived an edit but whose
// connection did not is treated as what it is — a different vCenter under a
// familiar label — and starts over with nothing.
func (m *Model) syncContexts() {
	fresh := m.backend.Contexts()
	old := m.byName
	states := make([]*contextState, 0, len(fresh))
	byName := make(map[string]*contextState, len(fresh))
	for _, cc := range fresh {
		st, ok := old[cc.Name]
		if ok {
			if !st.cc.SameConnection(cc) {
				st.reset()
				m.cache.Forget(cc.Name)
			}
			st.cc = cc
		} else {
			st = &contextState{cc: cc}
		}
		states = append(states, st)
		byName[cc.Name] = st
	}
	// A removed context leaves its inventory in the cache, where a context
	// later added under the same name would inherit it.
	for name := range old {
		if _, ok := byName[name]; !ok {
			m.cache.Forget(name)
		}
	}
	m.states, m.byName = states, byName
	m.searchDirty = true
	m.selected = clamp(m.selected, 0, max(0, len(m.states)-1))
	m.ctxCursor = clamp(m.ctxCursor, 0, max(0, len(m.states)-1))
	// A diagnosis of a context that no longer exists has nothing left to
	// report on, so it is dropped rather than left pointing at a removed one.
	if m.doctor != nil {
		if st, ok := m.byName[m.doctor.cc.Name]; !ok || st != m.doctor {
			m.doctor = nil
		}
	}
}

// selectByName puts a context by name in scope, if it exists.
func (m *Model) selectByName(name string) {
	for i, st := range m.states {
		if st.cc.Name == name {
			m.selected = i
			return
		}
	}
}

func (m *Model) busy() bool {
	for _, st := range m.states {
		if st.showsLoading() || st.diagging {
			return true
		}
	}
	return false
}

func (m *Model) applyInventory(msg inventoryMsg) tea.Cmd {
	st, ok := m.byName[msg.context]
	if !ok {
		return nil
	}
	if msg.cc != nil && st.cc != msg.cc {
		// The context was edited while this read was in flight: the answer is
		// about a vCenter this name no longer refers to. The reload the edit
		// asked for was suppressed by this very fetch still being in flight,
		// so issuing it here is what keeps the row from being left empty.
		st.loading = false
		if cmd := m.startLoad(st, true); cmd != nil {
			return tea.Batch(cmd, m.spin.Tick)
		}
		return nil
	}
	st.loading = false
	quiet := st.quiet
	st.quiet = false
	st.elapsed = msg.elapsed
	st.err = msg.err
	m.searchDirty = true
	// inv and loadedAt reflect the cache's last successful fetch, which on a
	// failed refresh is the same stale-but-real data the row already had —
	// never nil just because the latest attempt failed.
	wasEmpty := msg.err == nil && msg.inventory == nil
	switch {
	case msg.inventory != nil:
		st.inv = msg.inventory
	case wasEmpty:
		// A Backend contract violation (success with nothing to show) — a
		// broken implementation must not take the whole interface down with
		// it, so this renders as a plain empty result rather than a panic.
		st.inv = &vsphere.Inventory{Context: msg.context}
	}
	if msg.err == nil {
		st.loadedAt = msg.loadedAt
	}
	// A background refresh that worked says nothing: overwriting the message
	// line once a minute would bury whatever the operator was reading there.
	// One that failed always speaks, since silently serving data that has
	// stopped being updated is the failure mode worth avoiding.
	switch {
	case msg.err != nil && st.inv == nil:
		m.setMessage(msg.context+": "+msg.err.Error(), true)
	case msg.err != nil:
		m.setMessage(msg.context+": refresh failed, still showing data from "+st.loadedAt.Format("15:04:05")+": "+msg.err.Error(), true)
	case quiet:
		// Nothing to say: the table simply became current.
	case wasEmpty:
		m.setMessage(msg.context+" · nothing to show", false)
	default:
		note := ""
		if n := len(st.inv.Errors); n > 0 {
			note = fmt.Sprintf(" (%d listing error(s), see tabs)", n)
		}
		m.setMessage(msg.context+" · "+st.inv.Counts()+note, false)
	}
	m.clampCursor()
	if m.busy() {
		return m.spin.Tick
	}
	return nil
}

// setMessage sets the transient note under the table. An empty string clears
// it, which is what a scope change does: a note naming one vCenter is not
// describing the rows on screen any more.
func (m *Model) setMessage(s string, bad bool) {
	m.message = s
	m.messageBad = bad
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	if m.filtering {
		return m.handleFilterKey(msg)
	}
	// The form and its delete confirmation own every key while they are
	// open: both can hold free text, where a global "q" or "?" shortcut
	// would be a keystroke that silently never reaches the field it was
	// typed into.
	if m.mode == modeForm {
		return m.handleFormKey(msg)
	}
	if m.mode == modeConfirmDelete {
		return m.handleConfirmDeleteKey(msg)
	}
	if key.Matches(msg, m.keys.Quit) {
		m.quitting = true
		return tea.Quit
	}
	if key.Matches(msg, m.keys.Help) {
		if m.mode == modeHelp {
			m.mode = modeBrowse
		} else {
			m.mode = modeHelp
			m.detailY = 0
		}
		return nil
	}
	switch m.mode {
	case modeDetail:
		return m.handleDetailKey(msg)
	case modeDoctor:
		return m.handleDoctorKey(msg)
	case modeContexts:
		return m.handleContextsKey(msg)
	case modeSearch:
		return m.handleSearchKey(msg)
	case modeHelp:
		return m.handleHelpKey(msg)
	default:
		return m.handleBrowseKey(msg)
	}
}

// handleFormKey drives the add/edit form. Up and down move the row cursor;
// left and right change a select or a toggle; enter activates a button;
// every other key goes to the focused text field, including letters that are
// global shortcuts everywhere else in the interface.
func (m *Model) handleFormKey(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyCtrlC {
		m.quitting = true
		return tea.Quit
	}
	f := m.form
	if f == nil {
		m.mode = modeBrowse
		return nil
	}
	if msg.Type == tea.KeyEsc {
		return m.formCancel()
	}
	rows := f.rows()
	if len(rows) == 0 {
		return nil
	}
	switch msg.Type {
	case tea.KeyUp:
		f.cursor = clamp(f.cursor-1, 0, len(rows)-1)
		f.syncFocus()
		return nil
	case tea.KeyDown, tea.KeyTab:
		f.cursor = clamp(f.cursor+1, 0, len(rows)-1)
		f.syncFocus()
		return nil
	case tea.KeyShiftTab:
		f.cursor = clamp(f.cursor-1, 0, len(rows)-1)
		f.syncFocus()
		return nil
	}
	row := rows[clamp(f.cursor, 0, len(rows)-1)]
	switch row.kind {
	case rowSelect:
		switch msg.Type {
		case tea.KeyLeft:
			*row.idx = (*row.idx - 1 + len(row.options)) % len(row.options)
			f.syncFocus()
		case tea.KeyRight:
			*row.idx = (*row.idx + 1) % len(row.options)
			f.syncFocus()
		}
		return nil
	case rowToggle:
		switch {
		case msg.Type == tea.KeyLeft, msg.Type == tea.KeyRight, msg.Type == tea.KeyEnter, msg.String() == " ":
			*row.flag = !*row.flag
		}
		return nil
	case rowButton:
		if msg.Type == tea.KeyEnter {
			return row.action(m)
		}
		return nil
	case rowStatic:
		return nil
	default: // rowText, rowSecret
		var cmd tea.Cmd
		*row.input, cmd = row.input.Update(msg)
		return cmd
	}
}

// handleConfirmDeleteKey drives the delete confirmation screen. It has no
// free text, so it can use plain letters directly rather than the form's
// character-by-character dispatch.
func (m *Model) handleConfirmDeleteKey(msg tea.KeyMsg) tea.Cmd {
	if m.confirmDelete == nil {
		m.mode = modeBrowse
		return nil
	}
	switch msg.String() {
	case "y", "Y":
		name := m.confirmDelete.cc.Name
		also := m.confirmAlsoCredential
		return removeContext(m.ctx, m.backend, name, also)
	case "c", "C":
		m.confirmAlsoCredential = !m.confirmAlsoCredential
		return nil
	case "n", "N", "esc":
		m.confirmDelete = nil
		m.leaveOverlay()
		return nil
	}
	return nil
}

func (m *Model) handleFilterKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyTab:
		// Widening is offered exactly where the narrow filter runs out: the
		// same query, against every vCenter and every kind.
		m.toggleSearch()
		return nil
	case tea.KeyEnter:
		m.filtering = false
		m.filter.Blur()
		m.clampCursor()
		return nil
	case tea.KeyEsc:
		m.filtering = false
		m.filter.Blur()
		m.filter.SetValue("")
		m.clampCursor()
		return nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	// Filtering narrows the list under the cursor, so the cursor has to be
	// brought back into range on every keystroke, not only when it is done.
	m.cursor = 0
	m.offset = 0
	return cmd
}

func (m *Model) handleBrowseKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Kind):
		m.selectKindByNumber(msg.String())
	case key.Matches(msg, m.keys.Contexts):
		m.openContexts()
	case key.Matches(msg, m.keys.Search):
		return m.enterSearch()
	case key.Matches(msg, m.keys.NextTab):
		m.cycleTab(1)
	case key.Matches(msg, m.keys.PrevTab):
		m.cycleTab(-1)
	case key.Matches(msg, m.keys.Up):
		m.move(-1)
	case key.Matches(msg, m.keys.Down):
		m.move(1)
	case key.Matches(msg, m.keys.PageUp):
		m.move(-m.tableHeight())
	case key.Matches(msg, m.keys.PageDown):
		m.move(m.tableHeight())
	case key.Matches(msg, m.keys.Home):
		m.moveTo(0)
	case key.Matches(msg, m.keys.End):
		m.moveTo(1 << 30)
	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		// Focus returns whatever the cursor needs to animate itself, which is
		// nothing when blinking is off. Asking it beats hard-coding a blink.
		return m.filter.Focus()
	case key.Matches(msg, m.keys.Back):
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.clampCursor()
		}
	case key.Matches(msg, m.keys.AllScope):
		m.allScope = !m.allScope
		m.cursor, m.offset = 0, 0
		m.setMessage("", false)
		return m.enterScope()
	case key.Matches(msg, m.keys.Open):
		return m.open()
	case key.Matches(msg, m.keys.Reload):
		return tea.Batch(m.reload(false)...)
	case key.Matches(msg, m.keys.ReloadAll):
		return tea.Batch(m.reload(true)...)
	case key.Matches(msg, m.keys.Doctor):
		return m.diagnose()
	case key.Matches(msg, m.keys.Sort):
		m.sortMode = m.sortMode.next()
		m.cursor, m.offset = 0, 0
	}
	return nil
}

// selectKindByNumber maps the number row onto the resource kinds in the order
// the tab bar prints them, so what you read is what you press.
func (m *Model) selectKindByNumber(s string) {
	i, err := strconv.Atoi(s)
	if err != nil || i < 1 || i > len(vsphere.AllKinds) {
		return
	}
	k := vsphere.AllKinds[i-1]
	if k == m.kind {
		return
	}
	m.kind = k
	m.cursor, m.offset = 0, 0
}

// openContexts shows the vCenter list. With the sidebar gone this is the only
// place a context is switched, added, edited or removed, which is what keeps
// the browse screen down to eight keys.
func (m *Model) openContexts() {
	m.ctxCursor = clamp(m.selected, 0, max(0, len(m.states)-1))
	m.mode = modeContexts
}

// handleContextsKey drives the contexts screen.
func (m *Model) handleContextsKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.mode = modeBrowse
	case key.Matches(msg, m.keys.Up):
		m.moveContext(-1)
	case key.Matches(msg, m.keys.Down):
		m.moveContext(1)
	case key.Matches(msg, m.keys.Home):
		m.ctxCursor = 0
	case key.Matches(msg, m.keys.End):
		m.ctxCursor = max(0, len(m.states)-1)
	case key.Matches(msg, m.keys.UseContext):
		return m.useContext()
	case key.Matches(msg, m.keys.AllScope):
		m.allScope = true
		m.cursor, m.offset = 0, 0
		m.setMessage("", false)
		m.mode = modeBrowse
		return m.enterScope()
	case key.Matches(msg, m.keys.Reload):
		if st := m.contextAt(m.ctxCursor); st != nil {
			if cmd := m.startLoad(st, true); cmd != nil {
				return tea.Batch(cmd, m.spin.Tick)
			}
		}
	case key.Matches(msg, m.keys.Doctor):
		return m.diagnoseContext(m.contextAt(m.ctxCursor))
	case key.Matches(msg, m.keys.NewContext):
		m.returnTo = modeContexts
		return m.enterForm(nil)
	case key.Matches(msg, m.keys.EditContext):
		if st := m.contextAt(m.ctxCursor); st != nil {
			m.returnTo = modeContexts
			return m.enterForm(st)
		}
	case key.Matches(msg, m.keys.DeleteContext):
		if st := m.contextAt(m.ctxCursor); st != nil {
			m.confirmDelete = st
			m.confirmAlsoCredential = true
			m.returnTo = modeContexts
			m.mode = modeConfirmDelete
		}
	}
	return nil
}

func (m *Model) moveContext(delta int) {
	if len(m.states) == 0 {
		return
	}
	m.ctxCursor = clamp(m.ctxCursor+delta, 0, len(m.states)-1)
}

func (m *Model) contextAt(i int) *contextState {
	if i < 0 || i >= len(m.states) {
		return nil
	}
	return m.states[i]
}

// useContext narrows the view to the highlighted vCenter and returns to the
// table, which is what choosing one in a list is understood to mean.
func (m *Model) useContext() tea.Cmd {
	if len(m.states) == 0 {
		return nil
	}
	m.selected = clamp(m.ctxCursor, 0, len(m.states)-1)
	m.allScope = false
	m.cursor, m.offset = 0, 0
	m.setMessage("", false)
	m.mode = modeBrowse
	return m.enterScope()
}

// enterSearch opens the estate-wide results. With nothing typed yet it opens
// with the query focused, so "tab" is a way in from an empty table as well as
// a way to widen a filter that did not find enough.
func (m *Model) enterSearch() tea.Cmd {
	m.mode = modeSearch
	m.filter.Placeholder = "search every vCenter"
	m.cursor, m.offset = 0, 0
	if strings.TrimSpace(m.filter.Value()) == "" {
		m.filtering = true
		return m.filter.Focus()
	}
	m.filtering = false
	m.filter.Blur()
	return nil
}

// leaveSearch returns to the table with the query intact, because the filter
// and the search are the same query at two widths: narrowing back should not
// cost you what you typed.
func (m *Model) leaveSearch() {
	m.mode = modeBrowse
	m.filtering = false
	m.filter.Blur()
	m.filter.Placeholder = filterPlaceholder
	m.clampCursor()
}

func (m *Model) toggleSearch() tea.Cmd {
	if m.mode == modeSearch {
		m.leaveSearch()
		return nil
	}
	return m.enterSearch()
}

// handleSearchKey drives the estate-wide results. There is no scope to change
// here — the search is already every vCenter — so the keys are movement, "/"
// to refine the query in place, and the way back.
func (m *Model) handleSearchKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Search):
		m.leaveSearch()
	case key.Matches(msg, m.keys.Back):
		m.leaveSearch()
	case key.Matches(msg, m.keys.Up):
		m.move(-1)
	case key.Matches(msg, m.keys.Down):
		m.move(1)
	case key.Matches(msg, m.keys.PageUp):
		m.move(-m.tableHeight())
	case key.Matches(msg, m.keys.PageDown):
		m.move(m.tableHeight())
	case key.Matches(msg, m.keys.Home):
		m.moveTo(0)
	case key.Matches(msg, m.keys.End):
		m.moveTo(1 << 30)
	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		return m.filter.Focus()
	case key.Matches(msg, m.keys.Open):
		return m.open()
	case key.Matches(msg, m.keys.Sort):
		m.sortMode = m.sortMode.next()
		m.searchDirty = true
		m.cursor, m.offset = 0, 0
	case key.Matches(msg, m.keys.Reload):
		return tea.Batch(m.reload(false)...)
	case key.Matches(msg, m.keys.ReloadAll):
		return tea.Batch(m.reload(true)...)
	}
	return nil
}

// reload refetches what is in scope, or every configured context when all is
// set regardless of the current scope.
func (m *Model) reload(everything bool) []tea.Cmd {
	if !everything {
		return m.ensureLoaded(true)
	}
	was := m.allScope
	m.allScope = true
	cmds := m.ensureLoaded(true)
	m.allScope = was
	return cmds
}

func (m *Model) open() tea.Cmd {
	if _, ok := m.currentRow(); ok {
		m.detailFrom = m.mode
		m.mode = modeDetail
		m.detailY = 0
	}
	return nil
}

// diagnose walks the connection for the vCenter the cursor is on. In
// all-vCenters view that is the one the selected row came from, so "d" always
// asks about whatever produced the line you are looking at rather than about
// whichever context happens to be in scope.
func (m *Model) diagnose() tea.Cmd {
	return m.diagnoseContext(m.rowContext())
}

func (m *Model) diagnoseContext(st *contextState) tea.Cmd {
	if st == nil || st.diagging {
		return nil
	}
	st.diagging = true
	st.diag = nil
	m.doctor = st
	m.mode = modeDoctor
	return tea.Batch(diagnose(m.ctx, m.backend, st.cc), m.spin.Tick)
}

// rowContext is the vCenter behind the selected row, falling back to the one
// in scope when there is no row — an empty tab, or a context that never
// answered.
func (m *Model) rowContext() *contextState {
	if r, ok := m.currentRow(); ok {
		if st, ok := m.byName[r.context]; ok {
			return st
		}
	}
	return m.current()
}

// handleHelpKey scrolls the key reference. It shares detailY with the detail
// pane: the two are never open at once, and one scroll offset is one thing to
// reason about rather than two.
func (m *Model) handleHelpKey(msg tea.KeyMsg) tea.Cmd {
	limit := max(0, len(m.helpLines())-m.bodyHeight())
	switch {
	case key.Matches(msg, m.keys.Back):
		m.mode = modeBrowse
		m.detailY = 0
	case key.Matches(msg, m.keys.Up):
		m.detailY = clamp(m.detailY-1, 0, limit)
	case key.Matches(msg, m.keys.Down):
		m.detailY = clamp(m.detailY+1, 0, limit)
	case key.Matches(msg, m.keys.PageUp):
		m.detailY = clamp(m.detailY-m.bodyHeight(), 0, limit)
	case key.Matches(msg, m.keys.PageDown):
		m.detailY = clamp(m.detailY+m.bodyHeight(), 0, limit)
	case key.Matches(msg, m.keys.Home):
		m.detailY = 0
	case key.Matches(msg, m.keys.End):
		m.detailY = limit
	}
	return nil
}

func (m *Model) handleDetailKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.mode = m.detailFrom
	case key.Matches(msg, m.keys.Up):
		if m.detailY > 0 {
			m.detailY--
		}
	case key.Matches(msg, m.keys.Down):
		m.detailY++
	case key.Matches(msg, m.keys.NextTab), key.Matches(msg, m.keys.PrevTab):
		// Moving through the list from inside a detail pane keeps the pane
		// open, which is how you compare two VMs without going back and forth.
		delta := 1
		if key.Matches(msg, m.keys.PrevTab) {
			delta = -1
		}
		m.move(delta)
		m.detailY = 0
	}
	return nil
}

func (m *Model) handleDoctorKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.mode = modeBrowse
	case key.Matches(msg, m.keys.Reload):
		return m.diagnoseContext(m.doctor)
	}
	return nil
}

func (m *Model) cycleTab(delta int) {
	kinds := vsphere.AllKinds
	i := 0
	for n, k := range kinds {
		if k == m.kind {
			i = n
			break
		}
	}
	i = (i + delta + len(kinds)) % len(kinds)
	m.kind = kinds[i]
	m.cursor, m.offset = 0, 0
}

func (m *Model) move(delta int) {
	m.moveTo(m.cursor + delta)
}

func (m *Model) moveTo(i int) {
	n := len(m.visibleRows())
	if n == 0 {
		m.cursor, m.offset = 0, 0
		return
	}
	m.cursor = clamp(i, 0, n-1)
	m.scrollIntoView(n)
}

func (m *Model) scrollIntoView(n int) {
	h := m.tableHeight()
	if h <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	m.offset = clamp(m.offset, 0, max(0, n-h))
}

func (m *Model) clampCursor() {
	m.moveTo(m.cursor)
}

// pendingScope reports whether anything in scope is still being fetched, which
// the header shows rather than blanking the table.
func (m *Model) pendingScope() bool {
	for _, st := range m.inScope() {
		if st.showsLoading() {
			return true
		}
	}
	return false
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
