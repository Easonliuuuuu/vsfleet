package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vc-tui/internal/config"
	"github.com/easonliuuuuu/vc-tui/internal/session"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

// mode is which full-screen view is showing. Detail, diagnosis and help all
// replace the table rather than floating over it: a half-covered table invites
// you to read a number that is no longer the number you are looking at.
type mode int

const (
	modeBrowse mode = iota
	modeDetail
	modeDoctor
	modeHelp
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

func (s *contextState) rowStatus() rowStatus {
	switch {
	case s.loading || s.diagging:
		return statusWarn
	case s.err != nil:
		return statusBad
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
	case s.err != nil:
		return glyphFail
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
}

// Model is the whole interface. Every field is presentation state: nothing
// here is the only copy of anything, so the interface can be rewritten without
// consulting the rest of the program.
type Model struct {
	ctx     context.Context
	backend Backend
	keys    keyMap
	theme   theme
	spin    spinner.Model
	filter  textinput.Model

	states []*contextState
	byName map[string]*contextState

	selected  int
	pane      pane
	kind      vsphere.Kind
	allScope  bool
	filtering bool
	mode      mode

	cursor  int
	offset  int
	detailY int

	width, height int
	message       string
	messageBad    bool
	quitting      bool
}

// New builds the interface over a backend.
func New(ctx context.Context, backend Backend, opts Options) *Model {
	contexts := backend.Contexts()
	m := &Model{
		ctx:      ctx,
		backend:  backend,
		keys:     defaultKeys(),
		theme:    newTheme(),
		spin:     spinner.New(spinner.WithSpinner(spinner.Dot)),
		filter:   newFilterInput(),
		byName:   make(map[string]*contextState, len(contexts)),
		kind:     vsphere.KindVM,
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

// Init starts the spinner and loads whatever is in scope.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(append(m.ensureLoaded(false), m.spin.Tick)...)
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

// ensureLoaded returns load commands for everything in scope. force reloads
// contexts that already have an inventory.
func (m *Model) ensureLoaded(force bool) []tea.Cmd {
	var cmds []tea.Cmd
	for _, st := range m.inScope() {
		if st.loading {
			continue
		}
		if !force && (st.inv != nil || st.err != nil) {
			continue
		}
		st.loading = true
		st.err = nil
		cmds = append(cmds, loadInventory(m.ctx, m.backend, st.cc))
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
	if needle == "" {
		return out
	}
	kept := out[:0]
	for _, r := range out {
		if strings.Contains(strings.ToLower(r.name), needle) {
			kept = append(kept, r)
		}
	}
	return kept
}

// failuresInScope lists the contexts that could not be read, so the browse
// view can say so without the reader having to notice a missing row.
func (m *Model) failuresInScope() []*contextState {
	var out []*contextState
	for _, st := range m.inScope() {
		if st.err != nil {
			out = append(out, st)
		}
	}
	return out
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

	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
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
	st.loadedAt = time.Now()
	if msg.err != nil {
		st.err = msg.err
		st.inv = nil
		m.setMessage(msg.context+": "+msg.err.Error(), true)
	} else {
		st.err = nil
		st.inv = msg.inventory
		m.setMessage(msg.context+" · "+msg.inventory.Counts(), false)
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
