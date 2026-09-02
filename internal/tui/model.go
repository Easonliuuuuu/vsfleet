package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vc-tui/internal/cache"
	"github.com/easonliuuuuu/vc-tui/internal/config"
	"github.com/easonliuuuuu/vc-tui/internal/session"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

// maxConcurrentLoads bounds how many contexts fetch their inventory at once.
// Without a bound, an estate with dozens of contexts would open that many
// connections the instant the interface starts.
const maxConcurrentLoads = 4

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
)

// pane is which side of the browse view the arrow keys drive.
type pane int

const (
	paneContexts pane = iota
	paneResources
)

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
}

// A context can be both erroring and holding data at once — the cache keeps
// the last inventory that loaded successfully even when the most recent
// refresh failed, so that case gets its own status between "never connected"
// (bad) and "current" (good) rather than losing the stale data's presence.
func (s *contextState) rowStatus() rowStatus {
	switch {
	case s.loading || s.diagging:
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
	case s.loading || s.diagging:
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
	pane      pane
	kind      vsphere.Kind
	sortMode  sortMode
	allScope  bool
	filtering bool
	mode      mode

	cursor  int
	offset  int
	detailY int

	// form holds the add/edit context form while mode is modeForm.
	form *contextForm
	// confirmDelete is the context pending removal while mode is
	// modeConfirmDelete, and confirmAlsoCredential is whether its stored
	// password is removed along with it.
	confirmDelete         *contextState
	confirmAlsoCredential bool

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
		pane:     paneResources,
		width:    100,
		height:   30,
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

func newFilterInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "filter by name"
	ti.CharLimit = 128
	return ti
}

// Init starts the spinner and prefetches every configured context, selected
// one first. With no contexts configured yet there is nothing to load, so it
// opens the setup form instead of an empty table with no way to fill it.
func (m *Model) Init() tea.Cmd {
	if len(m.states) == 0 {
		return tea.Batch(m.enterForm(nil), m.spin.Tick)
	}
	return tea.Batch(m.prefetch()...)
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
	if st.loading {
		return nil
	}
	if !force && (st.inv != nil || st.err != nil) {
		return nil
	}
	st.loading = true
	return loadInventory(m.ctx, m.cache, m.backend, st.cc)
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
	rows := m.rows()
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

	case diagnosisMsg:
		if st, ok := m.byName[msg.context]; ok {
			st.diagging = false
			st.diag = msg.diagnosis
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
	m.mode = modeBrowse
	m.pane = paneResources
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
	m.mode = modeBrowse
	return nil
}

// syncContexts rebuilds the context list from the backend after a save or a
// removal, keeping the loaded inventory of every context that still exists.
func (m *Model) syncContexts() {
	fresh := m.backend.Contexts()
	old := m.byName
	states := make([]*contextState, 0, len(fresh))
	byName := make(map[string]*contextState, len(fresh))
	for _, cc := range fresh {
		st, ok := old[cc.Name]
		if ok {
			st.cc = cc
		} else {
			st = &contextState{cc: cc}
		}
		states = append(states, st)
		byName[cc.Name] = st
	}
	m.states, m.byName = states, byName
	m.selected = clamp(m.selected, 0, max(0, len(m.states)-1))
}

// selectByName moves the sidebar cursor onto a context by name, if it exists.
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
		if st.loading || st.diagging {
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
	st.loading = false
	st.elapsed = msg.elapsed
	st.err = msg.err
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
	switch {
	case msg.err != nil && st.inv == nil:
		m.setMessage(msg.context+": "+msg.err.Error(), true)
	case msg.err != nil:
		m.setMessage(msg.context+": refresh failed, still showing data from "+st.loadedAt.Format("15:04:05")+": "+msg.err.Error(), true)
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
		}
		return nil
	}
	switch m.mode {
	case modeDetail:
		return m.handleDetailKey(msg)
	case modeDoctor:
		return m.handleDoctorKey(msg)
	case modeHelp:
		if key.Matches(msg, m.keys.Back) {
			m.mode = modeBrowse
		}
		return nil
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
		m.mode = modeBrowse
		return removeContext(m.ctx, m.backend, name, also)
	case "c", "C":
		m.confirmAlsoCredential = !m.confirmAlsoCredential
		return nil
	case "n", "N", "esc":
		m.confirmDelete = nil
		m.mode = modeBrowse
		return nil
	}
	return nil
}

func (m *Model) handleFilterKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
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
	case key.Matches(msg, m.keys.NextPane):
		if m.pane == paneContexts {
			m.pane = paneResources
		} else {
			m.pane = paneContexts
		}
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
		return tea.Batch(m.ensureLoaded(false)...)
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
	case key.Matches(msg, m.keys.NewContext):
		return m.enterForm(nil)
	case key.Matches(msg, m.keys.EditContext):
		if st := m.current(); st != nil {
			return m.enterForm(st)
		}
	case key.Matches(msg, m.keys.DeleteContext):
		if st := m.current(); st != nil {
			m.confirmDelete = st
			m.confirmAlsoCredential = true
			m.mode = modeConfirmDelete
		}
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
	if m.pane == paneContexts {
		// Opening a context narrows the scope to it, which is what selecting
		// one in a sidebar is understood to mean everywhere else.
		m.allScope = false
		m.cursor, m.offset = 0, 0
		m.pane = paneResources
		return tea.Batch(m.ensureLoaded(false)...)
	}
	if _, ok := m.currentRow(); ok {
		m.mode = modeDetail
		m.detailY = 0
	}
	return nil
}

func (m *Model) diagnose() tea.Cmd {
	st := m.current()
	if st == nil || st.diagging {
		return nil
	}
	st.diagging = true
	st.diag = nil
	m.mode = modeDoctor
	return tea.Batch(diagnose(m.ctx, m.backend, st.cc), m.spin.Tick)
}

func (m *Model) handleDetailKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.mode = modeBrowse
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
		return m.diagnose()
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
	if m.pane == paneContexts {
		if len(m.states) == 0 {
			return
		}
		m.selected = clamp(m.selected+delta, 0, len(m.states)-1)
		if !m.allScope {
			m.cursor, m.offset = 0, 0
		}
		return
	}
	m.moveTo(m.cursor + delta)
}

func (m *Model) moveTo(i int) {
	n := len(m.rows())
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
		if st.loading {
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
