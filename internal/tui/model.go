package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/limiter"
	"github.com/easonliuuuuu/vsfleet/internal/session"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// maxConcurrentLoads bounds how many fetch groups run at once, across every
// context and every resource kind. Without a bound, an estate with dozens of
// contexts — each fetching up to six groups concurrently — would open that
// many connections the instant the interface starts.
const (
	maxConcurrentLoads = 4
	// Bubble Tea's default renderer flushes at 60 FPS. Waiting across several
	// frames makes the initial loading pane observable before fast credential
	// requests can replace it with their prompt overlay.
	initialPaintDelay = 50 * time.Millisecond
)

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
	modeChanges
	modeChangeDetail
	modeHistoryRuns
	modeHistoryTimeline
	modeHistoryTimelineDetail
	modeHistoryRunEdit
)

const (
	historyPaneChanges = iota
	historyPaneTrends
	historyPaneRuns
)

// searchState is one estate-wide search: every kind, every vCenter, matched
// on name the same way "vsfleet search" matches on the command line.
//
// It is answered from the inventories already in memory rather than by going
// back to the vCenters. A vCenter or kind that has not been selected or
// explicitly reloaded is reported as incomplete rather than quietly narrowing
// the result.
type searchState struct {
	query      string
	rows       []row
	searched   int
	missing    []*contextState
	incomplete []searchKind
}

// searchKind is a kind that search could not use as a complete source. It is
// separate from missing contexts because a context can have useful VM rows
// while its hosts, datastores, or another kind are still being fetched.
type searchKind struct {
	context *contextState
	kind    vsphere.Kind
	reason  string
	loading bool
}

// kindState is the cache and freshness record for one resource kind. VMs and
// templates share a fetch group, but keep separate records because search,
// tabs, and stale preservation address them as separate resource kinds.
type kindState struct {
	loaded     bool
	loading    bool
	attempted  bool
	loadedAt   time.Time
	err        error
	generation uint64
}

// contextState is one vCenter as the UI sees it: the configuration, the last
// inventory fetched from it, and whatever went wrong instead. A failed context
// keeps its row — that a customer environment is unreachable is information,
// not a reason to hide it.
type contextState struct {
	cc          *config.Context
	inv         *vsphere.Inventory
	kinds       map[vsphere.Kind]*kindState
	err         error
	loading     bool
	attempted   bool
	phase       contextPhase
	loadingKind vsphere.FetchGroup
	elapsed     time.Duration
	loadedAt    time.Time
	diag        *vsphere.Diagnosis
	diagging    bool
	// credentialPrompted records that the last failed load needed interactive
	// credential entry. A timer must not repeat that interaction; selecting or
	// explicitly reloading the context clears the gate and may ask again.
	credentialPrompted bool
	// allowCredentialPrompt distinguishes an operator-requested load from a
	// quiet timer refresh. A background refresh may use stored credentials or
	// an existing session, but it never gets to interrupt the screen for input.
	allowCredentialPrompt bool
	// quiet marks the in-flight read as one nobody asked for — the periodic
	// background refresh rather than a keystroke. It suppresses the pending
	// indicator and the success message, so a table being kept current does
	// not flicker through "connecting…" once a minute. Most quiet failures are
	// reported; one that merely needs input stays on the context row until the
	// operator explicitly selects or reloads it.
	quiet bool

	// outstanding counts messages truly in flight for the load currently
	// running, at every stage of it: 1 for the connect/index step, then 1
	// again for just the priority fetch group once that lands, then
	// len(vsphere.AllGroups)-1 once the priority group lands in turn and
	// the rest are dispatched. loading clears once it reaches 0 — see
	// finishLoad. A message that arrives for a context an edit has since
	// pointed elsewhere (cc no longer matches — see the checks in Update)
	// still counts down the same way, so the abandoned load is known to
	// have fully drained — whatever stage it was at when the edit
	// landed — before the edit's own retry, suppressed while it was still
	// in flight, actually starts. See dropStraggler.
	outstanding int
	// awaitingPriority is true from the moment the priority fetch group is
	// dispatched until it lands. applyGroup uses it to tell "the priority
	// group just landed, fan the rest out" apart from "one of the five
	// concurrent groups just landed, one fewer left to wait for" — a
	// distinction outstanding alone cannot make once an edit has changed
	// how many messages a load ever actually had in flight.
	awaitingPriority bool
	// startedAt is when beginLoad issued the connect/index command, for
	// elapsed: the total wall-clock time from asking to the last group
	// landing, not any one group's own share of it.
	startedAt time.Time
	// handle is the connected, index-built session the in-flight load's
	// fetch groups run through — see Backend.BeginInventory. It is nil
	// before the connect/index result lands and after the load finishes.
	handle InventoryHandle
	// stages carries advisory progress from the production backend. The channel
	// is bounded so reporting a stage can never stall authentication or loading.
	stages     chan vsphere.Stage
	stageDone  chan struct{}
	generation uint64
}

func newContextState(cc *config.Context) *contextState {
	s := &contextState{cc: cc, kinds: make(map[vsphere.Kind]*kindState, len(vsphere.AllKinds))}
	for _, kind := range vsphere.AllKinds {
		s.kinds[kind] = &kindState{}
	}
	return s
}

func (s *contextState) kind(kind vsphere.Kind) *kindState {
	if s.kinds == nil {
		s.kinds = make(map[vsphere.Kind]*kindState, len(vsphere.AllKinds))
	}
	if ks, ok := s.kinds[kind]; ok {
		return ks
	}
	ks := &kindState{}
	s.kinds[kind] = ks
	return ks
}

func (s *contextState) markGroupLoading(group vsphere.FetchGroup, generation uint64) {
	for _, kind := range kindsIn(group) {
		ks := s.kind(kind)
		ks.loading = true
		ks.attempted = true
		ks.generation = generation
	}
}

func (s *contextState) applyGroupState(group vsphere.FetchGroup, part *vsphere.Inventory, generation uint64, now time.Time) {
	for _, kind := range kindsIn(group) {
		ks := s.kind(kind)
		// The enclosing load generation is checked before this method is called.
		// Keep the per-kind generation too, so the cache record remains
		// self-describing and cannot be mistaken for a newer result.
		if ks.generation != 0 && ks.generation != generation {
			continue
		}
		ks.loading = false
		if part != nil {
			if message, failed := part.ErrorFor(kind); failed {
				ks.err = errors.New(message)
				continue
			}
		}
		ks.loaded = true
		ks.loadedAt = now
		ks.err = nil
	}
}

func (s *contextState) markAllKindsError(err error, generation uint64) {
	for _, kind := range vsphere.AllKinds {
		ks := s.kind(kind)
		ks.loading = false
		ks.attempted = true
		ks.generation = generation
		ks.err = err
	}
}

func (s *contextState) hasKindErrors() bool {
	for _, kind := range vsphere.AllKinds {
		if s.kind(kind).err != nil {
			return true
		}
	}
	return false
}

func (s *contextState) allKindsLoaded() bool {
	for _, kind := range vsphere.AllKinds {
		ks := s.kind(kind)
		if !ks.loaded || ks.err != nil {
			return false
		}
	}
	return true
}

// contextPhase is deliberately separate from inventory presence. A context
// may still show a previous inventory while its next authentication or refresh
// is waiting, and an untouched context must remain visibly disconnected rather
// than looking like a failed connection.
type contextPhase string

const (
	phaseIdle                 contextPhase = ""
	phaseCredentials          contextPhase = "credentials required"
	phaseWaitingCredentials   contextPhase = "waiting for credentials"
	phaseReadingKeyring       contextPhase = "reading keyring"
	phaseAuthenticating       contextPhase = "authenticating"
	phaseLoading              contextPhase = "loading"
	phaseReady                contextPhase = "ready"
	phaseAuthenticationFailed contextPhase = "authentication failed"
	phaseTimedOut             contextPhase = "timed out"
)

// reset drops everything this state learned from a vCenter, keeping only the
// configuration. It is what an edited context needs: the name is the same, so
// the row stays, but every inventory, error and diagnosis behind it came from
// a server this context no longer points at.
//
// loading, outstanding and handle are deliberately left alone. A fetch
// already in flight still holds the flag that stops a second one starting,
// and its messages are discarded on arrival by their context pointer or load
// generation rather than by clearing state here that would then be wrong.
func (s *contextState) reset() {
	s.stopStages()
	s.inv = nil
	s.kinds = make(map[vsphere.Kind]*kindState, len(vsphere.AllKinds))
	for _, kind := range vsphere.AllKinds {
		s.kinds[kind] = &kindState{}
	}
	s.err = nil
	s.attempted = false
	s.credentialPrompted = false
	s.allowCredentialPrompt = false
	s.phase = phaseIdle
	s.loadingKind = ""
	s.generation++
	s.elapsed = 0
	s.loadedAt = time.Time{}
	s.diag = nil
}

func (s *contextState) stopStages() {
	if s.stageDone != nil {
		close(s.stageDone)
		s.stageDone = nil
	}
	s.stages = nil
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
	case s.err != nil || s.hasKindErrors():
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
	// Credentials answers "prompt" credential references raised by
	// background loads with a masked overlay instead of a second stdin
	// reader racing Bubble Tea for keystrokes. Nil disables the overlay,
	// which is correct for callers — tests, the demo binary — whose backend
	// never has a context configured with a prompt credential.
	Credentials *PromptCoordinator

	// In and Out configure the streams Bubble Tea reads input from and writes
	// terminal output to. When nil, os.Stdin and os.Stdout are used.
	In  io.Reader
	Out io.Writer
	// Assessment is the optional persistent historical service.
	Assessment *assessment.Service
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
	// limiter bounds how many fetch groups run at once, across every
	// context and every kind — see maxConcurrentLoads.
	limiter *limiter.Limiter
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
	search           *searchState
	searchDirty      bool
	assessment       *assessment.Service
	runs             []assessment.Run
	changeDiff       *assessment.Diff
	historyErr       error
	changeCursor     int
	changeOffset     int
	runCursor        int
	capturing        bool
	baseRun          int64
	targetRun        int64
	pickerRole       string
	historyPane      int
	historyChurn     *assessment.ChurnTrend
	historySnapshots *assessment.SnapshotTrend
	historyCapacity  *assessment.CapacityTrend
	runEditInput     textinput.Model
	runEditKind      string
	runEditRunID     int64
	timeline         []assessment.VMHistoryEvent
	timelineCursor   int
	timelineOffset   int
	timelineAll      bool
	timelineQuery    string
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

	// credCoord answers "prompt" credential references raised by background
	// loads; see Options.Credentials. credPrompt is the overlay currently
	// showing one such request, if any, and while it is non-nil it owns
	// every keystroke ahead of the filter, the form, and every global
	// shortcut — see handleCredPromptKey.
	credCoord  *PromptCoordinator
	credPrompt *credPromptState

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
		ctx:          ctx,
		backend:      backend,
		limiter:      limiter.New(maxConcurrentLoads),
		keys:         defaultKeys(),
		theme:        newTheme(),
		spin:         spinner.New(spinner.WithSpinner(spinner.Dot)),
		filter:       newFilterInput(),
		runEditInput: newRunEditInput(),
		byName:       make(map[string]*contextState, len(contexts)),
		kind:         kind,
		sortMode:     sm,
		allScope:     opts.AllContexts,
		width:        100,
		height:       30,

		refreshInterval: refreshInterval(opts.RefreshInterval),
		credCoord:       opts.Credentials,
		assessment:      opts.Assessment,
	}
	for i, cc := range contexts {
		st := newContextState(cc)
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

// Init starts the spinner, loads only the selected context at start-up, and
// arms the background refresh. The selected load is held briefly so the first
// loading pane is flushed before a fast credential request can replace it with
// the password overlay. The all-contexts view is a presentation scope, not
// permission to contact every configured vCenter. With no contexts configured
// yet there is nothing to load, so it opens the setup form instead of an empty
// table with no way to fill it.
//
// A context not selected here is never contacted merely because the program
// started, the all-contexts view was opened, a search was opened, or a refresh
// timer fired. It loads when selected from the contexts screen, or after an
// explicit reload-all action.
func (m *Model) Init() tea.Cmd {
	if len(m.states) == 0 {
		return tea.Batch(m.enterForm(nil), m.spin.Tick)
	}
	startup := m.ensureSelectedLoaded(false)
	if cmd := m.nextCredPromptCmd(); cmd != nil {
		startup = append(startup, cmd)
	}
	cmds := []tea.Cmd{afterInitialPaint(m.ctx, startup)}
	if cmd := scheduleRefresh(m.refreshInterval); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// nextCredPromptCmd resumes listening for the next password ask. It is what
// lets a background load queued behind the one just resolved take its turn,
// and what arms the very first ask at start-up.
func (m *Model) nextCredPromptCmd() tea.Cmd {
	if m.credCoord == nil {
		return nil
	}
	return listenForCredRequest(m.credCoord.reqCh)
}

// credentialState finds the loading context behind a prompt request. Bare
// vCenter and proxy references are labeled with the context name by the
// connection path; explicit prompt labels are matched as a useful fallback.
func (m *Model) credentialState(label string) *contextState {
	for _, st := range m.states {
		if !st.loading {
			continue
		}
		labels := []string{st.cc.Name, st.cc.Name + " proxy", st.cc.Credential.Value, st.cc.Transport.Credential.Value}
		for _, candidate := range labels {
			if candidate != "" && candidate == label {
				return st
			}
		}
	}
	// A custom prompt reference carries no context identity. There can still
	// be only one active request because PromptCoordinator serializes them, so
	// use the first context currently resolving as the best available owner.
	for _, st := range m.states {
		if st.loading && (st.phase == phaseAuthenticating || st.phase == phaseReadingKeyring) {
			return st
		}
	}
	return nil
}

func (m *Model) markCredentialRequest(label string) {
	if st := m.credentialState(label); st != nil {
		st.credentialPrompted = true
		st.phase = phaseCredentials
		st.loadingKind = ""
	}
}

func (m *Model) markCredentialResumed(label string) {
	if st := m.credentialState(label); st != nil {
		st.phase = phaseAuthenticating
		st.loadingKind = ""
	}
}

// resolveCredPrompt answers the pending request and clears the overlay. The
// response channel is buffered by one, so this never blocks even when the
// background load already gave up on it because its own context canceled
// first.
func (m *Model) resolveCredPrompt(res credResult) {
	if m.credPrompt == nil {
		return
	}
	m.credPrompt.resp <- res
	m.credPrompt = nil
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
//
// It starts with the connect/index step alone (beginInventoryCmd); the
// priority fetch group is issued once that lands (the beginInventoryMsg case
// in Update), and the rest once the priority group lands in turn (the
// groupMsg case) — see the package doc on that progression.
func (m *Model) beginLoad(st *contextState, force, quiet bool) tea.Cmd {
	if st.loading {
		return nil
	}
	if !force && (st.inv != nil || st.err != nil) {
		return nil
	}
	st.loading = true
	st.generation++
	st.attempted = true
	st.allowCredentialPrompt = !quiet
	if !quiet {
		// A selection or explicit reload is a fresh opportunity to provide the
		// credential after a previous cancellation or rejection.
		st.credentialPrompted = false
	}
	st.phase = phaseAuthenticating
	st.loadingKind = ""
	st.quiet = quiet
	st.startedAt = time.Now()
	st.outstanding = 1
	st.awaitingPriority = false
	if _, ok := m.backend.(inventoryProgressBackend); !ok {
		return beginInventoryCmd(m.ctx, m.backend, st.cc, st.generation)
	}
	stageCh := make(chan vsphere.Stage, 16)
	st.stages = stageCh
	st.stageDone = make(chan struct{})
	done := st.stageDone
	report := func(stage vsphere.Stage) {
		select {
		case <-done:
			return
		default:
		}
		select {
		case stageCh <- stage:
		default:
		}
	}
	return tea.Batch(
		beginInventoryWithProgressCmd(m.ctx, m.backend, st.cc, report, st.generation),
		listenForStage(m.ctx, st.cc.Name, st.cc, st.generation, st.stages, done),
	)
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
	threshold := m.refreshInterval * backgroundRefreshFactor
	if onScreen {
		threshold = m.refreshInterval / 2
	}
	// The aggregate timestamp remains useful for a completed bundle and for
	// callers that only know the context-level state. A partial inventory must
	// also be due when any individual kind has never succeeded or has gone
	// stale, so one healthy kind cannot mask another kind's missing refresh.
	if !st.loadedAt.IsZero() && time.Since(st.loadedAt) >= threshold {
		return true
	}
	for _, kind := range vsphere.AllKinds {
		ks := st.kind(kind)
		if !ks.loaded || ks.loadedAt.IsZero() || time.Since(ks.loadedAt) >= threshold {
			return true
		}
	}
	return false
}

// refreshStale quietly re-reads every already-attempted context due for it. It
// covers loaded off-screen contexts as well as the one on screen — the header
// summary and an estate-wide search can answer from their cached data — but at
// two different rates: see refreshDue. Untouched contexts are skipped.
//
// The all-vCenters view makes already-loaded contexts visible together, so
// those contexts get the fast interval. It does not authenticate untouched
// contexts; an explicit reload-all is the action that requests those.
//
// Nothing here forces a read past one already in flight, so a vCenter slower
// than the interval simply refreshes less often instead of queueing work
// behind itself.
func (m *Model) refreshStale() []tea.Cmd {
	// A historical capture is an explicit operator action. Keep the live cache
	// quiet until it finishes so a background refresh cannot compete for the
	// same vCenter connections or make the changes screen jump underneath it.
	if m.capturing {
		return nil
	}
	if m.refreshInterval <= 0 {
		return nil
	}
	onScreen := make(map[*contextState]bool, len(m.states))
	for _, st := range m.inScope() {
		onScreen[st] = true
	}
	var cmds []tea.Cmd
	for _, st := range m.states {
		// A timer is not an operator selection. It may refresh a context that
		// has already been visited, but it must never be the first thing that
		// reads a keyring or opens a connection for an untouched context.
		if !st.attempted {
			continue
		}
		// Once a load has asked for interactive credentials, only another
		// explicit action may ask again. Without this gate a cancellation or
		// rejected password makes every refresh tick reopen the same prompt,
		// including for a context that is no longer selected.
		if st.credentialPrompted {
			continue
		}
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

// ensureSelectedLoaded is the lazy entry point used by start-up and context
// switching. It intentionally ignores allScope: showing all rows does not
// authorize contacting every configured vCenter.
func (m *Model) ensureSelectedLoaded(force bool) []tea.Cmd {
	st := m.current()
	if st == nil {
		return nil
	}
	if st.credentialPrompted && !st.loading {
		force = true
	}
	if cmd := m.startLoad(st, force); cmd != nil {
		return []tea.Cmd{cmd, m.spin.Tick}
	}
	return nil
}

// enterScope is what every change of what is on screen runs. It ensures the
// selected context has been attempted, while reusing a live in-process session
// and cached inventory when switching back to a context already loaded.
func (m *Model) enterScope() tea.Cmd {
	return tea.Batch(m.ensureSelectedLoaded(false)...)
}

// ensureAllLoaded is reserved for an explicit reload-all action. Presentation
// changes such as widening the scope or opening search call no network APIs.
func (m *Model) ensureAllLoaded(force bool) []tea.Cmd {
	was := m.allScope
	m.allScope = true
	cmds := m.ensureLoaded(force)
	m.allScope = was
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
		searchedContext := false
		for _, k := range vsphere.AllKinds {
			ks := cs.kind(k)
			if ks.loaded {
				searchedContext = true
				if q != "" {
					for _, r := range rowsFor(cs.inv, k, false) {
						if strings.Contains(strings.ToLower(r.name), q) {
							st.rows = append(st.rows, r)
						}
					}
				}
			}
			if ks.loading {
				st.incomplete = append(st.incomplete, searchKind{
					context: cs, kind: k, reason: "still loading", loading: true,
				})
			} else if ks.err != nil {
				st.incomplete = append(st.incomplete, searchKind{
					context: cs, kind: k, reason: firstLine(ks.err.Error()),
				})
			} else if !ks.loaded {
				st.incomplete = append(st.incomplete, searchKind{
					context: cs, kind: k, reason: "not loaded",
				})
			}
		}
		if searchedContext {
			st.searched++
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

	case beginInventoryMsg:
		return m, m.applyBeginInventory(msg)

	case groupMsg:
		return m, m.applyGroup(msg)

	case stageMsg:
		return m, m.applyStage(msg)

	case credRequestMsg:
		// A second concurrent ask cannot arrive here: the coordinator only
		// hands out another request after this one is resolved and the
		// model asks to listen again, see nextCredPromptCmd.
		st := m.credentialState(msg.req.label)
		m.markCredentialRequest(msg.req.label)
		if st != nil && !st.allowCredentialPrompt {
			// Quiet refreshes never acquire keyboard ownership. Answering the
			// buffered channel here lets the load finish and leaves the context
			// visibly waiting for an explicit selection/reload.
			msg.req.resp <- credResult{err: errBackgroundCredentialPrompt}
			return m, m.nextCredPromptCmd()
		}
		m.credPrompt = newCredPromptState(msg.req)
		return m, nil

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

	case historyRunsMsg:
		m.runs, m.historyErr = msg.runs, msg.err
		if m.historyErr == nil {
			m.loadDefaultHistoryDiff()
			return m, m.historyDiffCommand()
		}
		return m, nil
	case historyDiffMsg:
		m.changeDiff, m.historyErr = msg.diff, msg.err
		m.changeCursor, m.changeOffset = 0, 0
		return m, nil
	case historyTrendsMsg:
		m.historyErr = msg.err
		if msg.err == nil {
			m.historyChurn = &msg.churn
			m.historySnapshots = &msg.snapshots
			m.historyCapacity = &msg.capacity
		}
		return m, nil
	case historyRunUpdatedMsg:
		if msg.err != nil {
			m.historyErr = msg.err
			return m, nil
		}
		for i := range m.runs {
			if m.runs[i].ID == msg.run.ID {
				m.runs[i] = msg.run
				break
			}
		}
		m.runEditInput.Blur()
		m.mode = modeChanges
		m.runEditKind, m.runEditRunID = "", 0
		m.setMessage(fmt.Sprintf("assessment %d metadata updated", msg.run.ID), false)
		return m, nil
	case historyCaptureMsg:
		m.capturing = false
		if msg.err != nil {
			m.historyErr = msg.err
			return m, nil
		}
		m.setMessage(fmt.Sprintf("assessment %d saved (%s)", msg.run.ID, msg.run.Status), msg.run.Status == assessment.RunPartial)
		return m, tea.Batch(loadHistoryRunsCmd(m.ctx, m.assessment), loadHistoryTrendsCmd(m.ctx, m.assessment))
	case historyTimelineMsg:
		m.timeline, m.historyErr = msg.events, msg.err
		m.timelineCursor, m.timelineOffset = 0, 0
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
			}
			st.cc = cc
		} else {
			st = newContextState(cc)
		}
		states = append(states, st)
		byName[cc.Name] = st
	}
	for name, st := range old {
		if _, ok := byName[name]; !ok {
			st.stopStages()
		}
	}
	// A removed context's contextState is simply dropped along with it: its
	// inventory lived on the struct itself, not in a separate cache keyed by
	// name, so a context later added under the same name starts with nothing
	// rather than inheriting what the removed one had.
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

func (m *Model) applyStage(msg stageMsg) tea.Cmd {
	st, ok := m.byName[msg.context]
	if !ok || !st.loading || (msg.generation != 0 && st.generation != msg.generation) || (msg.cc != nil && st.cc != msg.cc) {
		return nil
	}
	switch msg.stage {
	case vsphere.StageResolvingCredentials:
		st.phase = phaseReadingKeyring
		st.loadingKind = ""
	case vsphere.StageAuthenticating:
		st.phase = phaseAuthenticating
		st.loadingKind = ""
	case vsphere.StageLoadingIndex:
		st.phase = phaseLoading
		st.loadingKind = ""
	case vsphere.StageLoadingVMs:
		st.phase = phaseLoading
		st.loadingKind = vsphere.GroupVMs
	case vsphere.StageLoadingHosts:
		st.phase = phaseLoading
		st.loadingKind = vsphere.GroupHosts
	case vsphere.StageLoadingClusters:
		st.phase = phaseLoading
		st.loadingKind = vsphere.GroupClusters
	case vsphere.StageLoadingVApps:
		st.phase = phaseLoading
		st.loadingKind = vsphere.GroupVApps
	case vsphere.StageLoadingDatastores:
		st.phase = phaseLoading
		st.loadingKind = vsphere.GroupDatastores
	case vsphere.StageLoadingNetworks:
		st.phase = phaseLoading
		st.loadingKind = vsphere.GroupNetworks
	}
	if st.stages == nil {
		return nil
	}
	return listenForStage(m.ctx, msg.context, st.cc, msg.generation, st.stages, st.stageDone)
}

// applyBeginInventory lands the connect/index result a load starts with. On
// success it kicks off the priority fetch group — the currently visible
// kind's — alone; the rest follow once that group's own groupMsg lands (see
// applyGroup). On failure, no group was ever attempted and the load ends
// here.
func (m *Model) applyBeginInventory(msg beginInventoryMsg) tea.Cmd {
	st, ok := m.byName[msg.context]
	if !ok {
		return nil
	}
	if (msg.generation != 0 && st.generation != msg.generation) || (msg.cc != nil && st.cc != msg.cc) {
		return m.dropStraggler(st)
	}
	st.outstanding-- // the connect/index step itself just landed
	if msg.err != nil {
		st.err = msg.err
		st.markAllKindsError(msg.err, st.generation)
		if st.credentialPrompted {
			st.phase = phaseCredentials
		} else {
			st.phase = phaseForLoadError(msg.err)
		}
		return m.finishLoad(st)
	}
	st.err = nil
	st.handle = msg.handle
	st.phase = phaseLoading
	st.loadingKind = vsphere.GroupFor(m.kind)
	st.outstanding = 1 // just the priority group, dispatched below
	st.awaitingPriority = true
	priority := vsphere.GroupFor(m.kind)
	st.markGroupLoading(priority, st.generation)
	return fetchGroupCmd(m.ctx, m.limiter, msg.handle, st.cc, priority, st.generation)
}

// applyGroup lands one fetch group's result, merging it into st.inv (see
// vsphere.Inventory.ApplyGroup — a group that failed keeps whatever that
// kind already had, rather than blanking it). The first group to land for a
// load is always the priority one dispatched by applyBeginInventory — see
// contextState.awaitingPriority — which is what makes this the moment to fan
// the remaining five out concurrently; every group after that just counts
// down toward finishLoad.
func (m *Model) applyGroup(msg groupMsg) tea.Cmd {
	st, ok := m.byName[msg.context]
	if !ok {
		return nil
	}
	if (msg.generation != 0 && st.generation != msg.generation) || (msg.cc != nil && st.cc != msg.cc) {
		return m.dropStraggler(st)
	}
	st.outstanding--
	if st.inv == nil {
		st.inv = &vsphere.Inventory{Context: msg.context}
	}
	st.inv.ApplyGroup(msg.group, msg.inv)
	st.applyGroupState(msg.group, msg.inv, msg.generation, time.Now())
	st.phase = phaseLoading
	st.loadingKind = msg.group
	m.searchDirty = true
	if st.awaitingPriority {
		st.awaitingPriority = false
		var cmds []tea.Cmd
		for _, g := range vsphere.AllGroups {
			if g == msg.group {
				continue
			}
			st.markGroupLoading(g, st.generation)
			cmds = append(cmds, fetchGroupCmd(m.ctx, m.limiter, st.handle, st.cc, g, st.generation))
		}
		st.outstanding = len(vsphere.AllGroups) - 1 // the ones just dispatched
		return tea.Batch(cmds...)
	}
	if st.outstanding > 0 {
		return nil
	}
	return m.finishLoad(st)
}

// dropStraggler discards one message from a load an edit has since
// superseded — st.cc no longer matches the cc it was issued for — counting
// it toward the abandoned load's outstanding total regardless. Once every
// straggler has landed this way, the edit's own reload — suppressed by
// beginLoad's loading guard while the old load was still in flight — is
// what actually runs, against the context's current configuration.
func (m *Model) dropStraggler(st *contextState) tea.Cmd {
	st.outstanding--
	if st.outstanding > 0 {
		return nil
	}
	st.loading = false
	st.awaitingPriority = false
	if cmd := m.startLoad(st, true); cmd != nil {
		return tea.Batch(cmd, m.spin.Tick)
	}
	return nil
}

// finishLoad runs once a load has nothing left outstanding — either
// applyBeginInventory failed outright, or applyGroup just landed the last
// fetch group — and reports it the same way a single-shot fetch used to:
// silently for a background refresh that worked, otherwise with what
// changed or what went wrong.
func (m *Model) finishLoad(st *contextState) tea.Cmd {
	st.loading = false
	quiet := st.quiet
	st.quiet = false
	st.elapsed = time.Since(st.startedAt)
	st.handle = nil
	st.stopStages()
	if st.err == nil {
		st.credentialPrompted = false
		if st.allKindsLoaded() {
			st.loadedAt = time.Now()
		}
		st.phase = phaseReady
		st.loadingKind = ""
	} else {
		st.loadingKind = ""
	}
	// A background refresh that worked says nothing: overwriting the message
	// line once a minute would bury whatever the operator was reading there.
	// One that failed always speaks, since silently serving data that has
	// stopped being updated is the failure mode worth avoiding.
	switch {
	case quiet && st.credentialPrompted:
		// A timer discovering that input is required is represented on the
		// context row, not as an unsolicited prompt or transient error banner.
	case st.err != nil && st.inv == nil:
		m.setMessage(st.cc.Name+": "+st.err.Error(), true)
	case st.err != nil:
		m.setMessage(st.cc.Name+": refresh failed, still showing data from "+st.loadedAt.Format("15:04:05")+": "+st.err.Error(), true)
	case quiet:
		// Nothing to say: the table simply became current.
	default:
		note := ""
		if n := len(st.inv.Errors); n > 0 {
			note = fmt.Sprintf(" (%d listing error(s), see tabs)", n)
		}
		m.setMessage(st.cc.Name+" · "+st.inv.Counts()+note, false)
	}
	m.clampCursor()
	if m.busy() {
		return m.spin.Tick
	}
	return nil
}

func phaseForLoadError(err error) contextPhase {
	if errors.Is(err, context.DeadlineExceeded) {
		return phaseTimedOut
	}
	if errors.Is(err, errPromptCanceled) || strings.Contains(strings.ToLower(err.Error()), "credential") || strings.Contains(strings.ToLower(err.Error()), "authenticate") || strings.Contains(strings.ToLower(err.Error()), "canceled") {
		return phaseAuthenticationFailed
	}
	return phaseAuthenticationFailed
}

// setMessage sets the transient note under the table. An empty string clears
// it, which is what a scope change does: a note naming one vCenter is not
// describing the rows on screen any more.
func (m *Model) setMessage(s string, bad bool) {
	m.message = s
	m.messageBad = bad
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	// The credential overlay owns every key ahead of anything else, filter
	// and form included: it can appear over any screen, since it answers a
	// background load rather than something the operator opened. Letting a
	// shortcut like "q" fall through here instead of into the password field
	// is the exact race this overlay exists to close (issue #26).
	if m.credPrompt != nil {
		return m.handleCredPromptKey(msg)
	}
	if msg.Type == tea.KeyCtrlC {
		m.quitting = true
		return tea.Quit
	}
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
	if m.mode == modeHistoryRunEdit {
		return m.handleHistoryRunEditKey(msg)
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
	case modeChanges:
		return m.handleChangesKey(msg)
	case modeChangeDetail:
		if key.Matches(msg, m.keys.Back) {
			m.mode = modeChanges
		}
		return nil
	case modeHistoryRuns:
		return m.handleHistoryRunsKey(msg)
	case modeHistoryRunEdit:
		return m.handleHistoryRunEditKey(msg)
	case modeHistoryTimeline:
		return m.handleHistoryTimelineKey(msg)
	case modeHistoryTimelineDetail:
		if key.Matches(msg, m.keys.Back) {
			m.mode = modeHistoryTimeline
		}
		return nil
	case modeHelp:
		return m.handleHelpKey(msg)
	default:
		return m.handleBrowseKey(msg)
	}
}

// handleCredPromptKey drives the credential overlay. Enter answers the
// pending request with what was typed; esc answers it with cancellation and
// keeps the interface open, moving on to whatever the failed load reports;
// ctrl+c also cancels it but quits outright, matching ctrl+c's meaning
// everywhere else in the interface rather than being one more shortcut this
// screen swallows.
func (m *Model) handleCredPromptKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyCtrlC:
		m.resolveCredPrompt(credResult{err: errPromptCanceled})
		m.quitting = true
		return tea.Quit
	case tea.KeyEsc:
		m.markCredentialResumed(m.credPrompt.label)
		m.resolveCredPrompt(credResult{err: errPromptCanceled})
		return m.nextCredPromptCmd()
	case tea.KeyEnter:
		m.markCredentialResumed(m.credPrompt.label)
		m.resolveCredPrompt(credResult{password: m.credPrompt.input.Value()})
		return m.nextCredPromptCmd()
	}
	var cmd tea.Cmd
	m.credPrompt.input, cmd = m.credPrompt.input.Update(msg)
	return cmd
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
		return m.toggleSearch()
	case tea.KeyEnter:
		m.filtering = false
		m.filter.Blur()
		m.clampCursor()
		return nil
	case tea.KeyEsc:
		if m.mode == modeSearch {
			m.leaveSearch()
			return nil
		}
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
	case key.Matches(msg, m.keys.History):
		return m.enterChanges()
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
// a way to widen a filter that did not find enough. The result is cache-only:
// untouched contexts remain "not searched" until selected or explicitly
// reloaded.
func (m *Model) enterSearch() tea.Cmd {
	m.mode = modeSearch
	m.filter.Placeholder = "search every vCenter"
	m.cursor, m.offset = 0, 0
	// Search is answered from the cache. Opening it must not turn an
	// estate-wide view into an estate-wide login storm; untouched contexts are
	// listed as "not searched" until the operator selects or explicitly reloads
	// them.
	var cmds []tea.Cmd
	if strings.TrimSpace(m.filter.Value()) == "" {
		m.filtering = true
		cmds = append(cmds, m.filter.Focus())
	} else {
		m.filtering = false
		m.filter.Blur()
	}
	return tea.Batch(cmds...)
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

// reload refetches the selected context, or every configured context when the
// caller requests an explicit all-contexts reload.
func (m *Model) reload(everything bool) []tea.Cmd {
	if !everything {
		// Lowercase r always means the selected context. In all-context and
		// estate-search views, widening the presentation must not silently
		// widen credential access too; uppercase R is the explicit all-context
		// operation.
		return m.ensureSelectedLoaded(true)
	}
	return m.ensureAllLoaded(true)
}

func (m *Model) open() tea.Cmd {
	r, ok := m.currentRow()
	if !ok {
		return nil
	}
	if m.mode == modeSearch {
		// A search result can come from a different context and kind than the
		// table behind the search. Make that result the real browse selection
		// before opening it, so esc lands on the vApp tab (or the corresponding
		// tab for another kind) instead of returning to a stale search scope.
		m.selectByName(r.context)
		m.allScope = false
		m.kind = r.kind
		m.cursor, m.offset = 0, 0
		for i, candidate := range m.rows() {
			if candidate.key == r.key {
				m.cursor = i
				break
			}
		}
		m.scrollIntoView(len(m.rows()))
		m.detailFrom = modeBrowse
	} else {
		m.detailFrom = m.mode
	}
	m.mode = modeDetail
	m.detailY = 0
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
	case key.Matches(msg, m.keys.Timeline):
		row, ok := m.currentRow()
		if !ok || row.kind != vsphere.KindVM || m.assessment == nil {
			return nil
		}
		m.timelineQuery = row.name
		m.timelineAll, m.timelineCursor, m.timelineOffset = false, 0, 0
		m.historyErr = nil
		m.mode = modeHistoryTimeline
		return loadHistoryTimelineCmd(m.ctx, m.assessment, row.name, false, false)
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
