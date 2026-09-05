package tui

import (
	"context"
	"fmt"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/contextops"
	"github.com/easonliuuuuu/vsfleet/internal/health"
	"github.com/easonliuuuuu/vsfleet/internal/limiter"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// beginInventoryMsg carries the outcome of connecting to one vCenter and
// building its shared path index — see Backend.BeginInventory. Success
// hands back a handle the model uses to fetch the priority group next (see
// fetchGroupCmd); failure means no group was ever attempted.
//
// cc is the configuration the connect was issued against: a context edited
// while this was in flight is a different vCenter by the time the answer
// lands, so the result is matched against it and dropped (its slot in
// contextState.outstanding still counted down, so the load it superseded is
// known to have fully drained) if it no longer applies — see the case in
// Update.
type beginInventoryMsg struct {
	context    string
	cc         *config.Context
	generation uint64
	handle     InventoryHandle
	err        error
}

// groupMsg carries the outcome of retrieving one fetch group for one
// vCenter. inv is never nil: a group that failed to list reports it through
// inv.Errors (see vsphere.Client.FetchGroup), not as a separate error here.
//
// context and cc serve the same purpose as they do on beginInventoryMsg.
type groupMsg struct {
	context    string
	cc         *config.Context
	generation uint64
	group      vsphere.FetchGroup
	inv        *vsphere.Inventory
}

// groupPageMsg carries one page of a fetch group's objects, ahead of the
// group's own completion. It exists so that an estate large enough to take
// minutes to read shows its first rows in seconds instead of showing nothing
// until the last one lands. pages is the stream it came from, handed back so
// the model can ask for the next one.
//
// A page is a preview and nothing more: the group's own groupMsg still
// carries the complete, authoritative result, and pages may be dropped when
// the interface cannot keep up with them.
type groupPageMsg struct {
	context    string
	cc         *config.Context
	generation uint64
	group      vsphere.FetchGroup
	inv        *vsphere.Inventory
	pages      chan *vsphere.Inventory
}

// stageMsg carries live progress from the production connection path. It is
// advisory: a backend that does not implement the optional progress extension
// still gets the same correct load behavior, just with coarser labels.
type stageMsg struct {
	context    string
	cc         *config.Context
	generation uint64
	stage      vsphere.Stage
}

// refreshTickMsg is the periodic wake-up that re-reads inventory nobody has
// asked about. It carries nothing: what is due for a re-read is decided when
// it lands, against the state as it stands then, not when it was scheduled.
type refreshTickMsg struct{}

type historyRunsMsg struct {
	runs []assessment.Run
	err  error
}

type historyDiffMsg struct {
	diff *assessment.Diff
	err  error
}

type historyCaptureMsg struct {
	run assessment.Run
	err error
}

type historyTimelineMsg struct {
	events []assessment.VMHistoryEvent
	err    error
}

type historyRunUpdatedMsg struct {
	run assessment.Run
	err error
}

type historyTrendsMsg struct {
	churn     assessment.ChurnTrend
	snapshots assessment.SnapshotTrend
	capacity  assessment.CapacityTrend
	err       error
}

type historyHealthMsg struct {
	report health.Report
	err    error
}

func loadHistoryRunsCmd(ctx context.Context, service *assessment.Service) tea.Cmd {
	return func() tea.Msg { runs, err := service.Runs(ctx); return historyRunsMsg{runs: runs, err: err} }
}

func loadHistoryDiffCmd(ctx context.Context, service *assessment.Service, base, target int64) tea.Cmd {
	return func() tea.Msg {
		d, err := service.Diff(ctx, base, target, false)
		return historyDiffMsg{diff: &d, err: err}
	}
}

func captureHistoryCmd(ctx context.Context, service *assessment.Service, contexts []*config.Context) tea.Cmd {
	return func() tea.Msg {
		run, err := service.Capture(ctx, assessment.CaptureOptions{Contexts: contexts, Source: "tui"})
		return historyCaptureMsg{run: run, err: err}
	}
}

func loadHistoryTimelineCmd(ctx context.Context, service *assessment.Service, query string, all, runtime bool) tea.Cmd {
	return func() tea.Msg {
		events, err := service.Timeline(ctx, query, "", all, runtime)
		return historyTimelineMsg{events: events, err: err}
	}
}

func loadHistoryTrendsCmd(ctx context.Context, service *assessment.Service) tea.Cmd {
	return func() tea.Msg {
		opts := assessment.TrendOptions{Limit: 30}
		churn, err := service.ChurnTrend(ctx, opts)
		if err != nil {
			return historyTrendsMsg{err: err}
		}
		snapshots, err := service.SnapshotTrend(ctx, opts, 30*24*time.Hour)
		if err != nil {
			return historyTrendsMsg{err: err}
		}
		capacity, err := service.CapacityTrend(ctx, opts, []string{"host", "cluster", "datastore"})
		return historyTrendsMsg{churn: churn, snapshots: snapshots, capacity: capacity, err: err}
	}
}

func loadHistoryHealthCmd(ctx context.Context, service *assessment.Service, runID int64, opts health.Options) tea.Cmd {
	return func() tea.Msg {
		if service == nil || service.Store == nil {
			return historyHealthMsg{err: fmt.Errorf("historical assessments are unavailable")}
		}
		if runID == 0 {
			var err error
			runID, err = service.Store.ResolveRun(ctx, "latest")
			if err != nil {
				return historyHealthMsg{err: err}
			}
		}
		data, err := service.Store.LoadExportData(ctx, runID)
		if err != nil {
			return historyHealthMsg{err: err}
		}
		report := health.Evaluate(data, opts)
		return historyHealthMsg{report: report}
	}
}

func updateHistoryRunCmd(ctx context.Context, service *assessment.Service, runID int64, field, value string) tea.Cmd {
	return func() tea.Msg {
		var label, note *string
		if field == "label" {
			label = &value
		} else {
			note = &value
		}
		run, err := service.Store.UpdateRunFields(ctx, strconv.FormatInt(runID, 10), label, note, nil)
		return historyRunUpdatedMsg{run: run, err: err}
	}
}

func toggleHistoryRunPinCmd(ctx context.Context, service *assessment.Service, run assessment.Run) tea.Cmd {
	return func() tea.Msg {
		value := !run.Pinned
		updated, err := service.Store.UpdateRunFields(ctx, strconv.FormatInt(run.ID, 10), nil, nil, &value)
		return historyRunUpdatedMsg{run: updated, err: err}
	}
}

// scheduleRefresh arms the next background refresh. Each tick arms the one
// after it rather than a repeating ticker, so a refresh that takes longer
// than the interval cannot have the next one queued up behind it.
func scheduleRefresh(d time.Duration) tea.Cmd {
	if d <= 0 {
		return nil
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

// afterInitialPaint holds startup work until the renderer has had time to
// flush the model's first view. Bubble Tea buffers writes and renders them on
// its ticker, so an immediately-resolving credential request could otherwise
// replace the loading pane before it was ever visible to the operator.
func afterInitialPaint(ctx context.Context, cmds []tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		timer := time.NewTimer(initialPaintDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
			batch := tea.Batch(cmds...)
			if batch == nil {
				return nil
			}
			return batch()
		case <-ctx.Done():
			return nil
		}
	}
}

// diagnosisMsg carries a completed connection diagnosis. cc serves the same
// purpose as it does on beginInventoryMsg: a diagnosis of the endpoint a
// context used to have says nothing about the one it has now.
type diagnosisMsg struct {
	context   string
	cc        *config.Context
	diagnosis *vsphere.Diagnosis
}

// beginInventoryCmd connects to one vCenter and builds its shared path
// index in the background. Landing this is what the model uses to kick off
// the priority fetch group — see (*Model).beginLoad and the beginInventoryMsg
// case in Update.
func beginInventoryCmd(ctx context.Context, b Backend, cc *config.Context, generations ...uint64) tea.Cmd {
	var generation uint64
	if len(generations) > 0 {
		generation = generations[0]
	}
	return func() tea.Msg {
		handle, err := b.BeginInventory(ctx, cc)
		return beginInventoryMsg{context: cc.Name, cc: cc, generation: generation, handle: handle, err: err}
	}
}

type inventoryProgressBackend interface {
	BeginInventoryWithProgress(context.Context, *config.Context, func(vsphere.Stage)) (InventoryHandle, error)
}

func beginInventoryWithProgressCmd(ctx context.Context, b Backend, cc *config.Context, report func(vsphere.Stage), generations ...uint64) tea.Cmd {
	var generation uint64
	if len(generations) > 0 {
		generation = generations[0]
	}
	return func() tea.Msg {
		var (
			handle InventoryHandle
			err    error
		)
		if pb, ok := b.(inventoryProgressBackend); ok {
			handle, err = pb.BeginInventoryWithProgress(ctx, cc, report)
		} else {
			handle, err = b.BeginInventory(ctx, cc)
		}
		return beginInventoryMsg{context: cc.Name, cc: cc, generation: generation, handle: handle, err: err}
	}
}

func listenForStage(ctx context.Context, contextName string, cc *config.Context, generation uint64, ch <-chan vsphere.Stage, done <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		select {
		case stage := <-ch:
			return stageMsg{context: contextName, cc: cc, generation: generation, stage: stage}
		case <-done:
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

// fetchGroupCmd retrieves one fetch group through an already-connected
// handle, bounded by lim so a context fetching all six of its groups at
// once — or an estate with several contexts doing the same — does not open
// unbounded connections at the same moment. Every group for one context gets
// its own command, so several run concurrently (bounded by lim) and a slow
// one only ever delays its own kind.
func fetchGroupCmd(ctx context.Context, lim *limiter.Limiter, handle InventoryHandle, cc *config.Context, group vsphere.FetchGroup, generations ...uint64) tea.Cmd {
	var generation uint64
	if len(generations) > 0 {
		generation = generations[0]
	}
	// Buffered so the retrieval never waits on the render loop, and dropping
	// rather than blocking when it is full: a page the interface is too busy
	// to show is not worth slowing the fetch down for, since the group's own
	// result carries everything regardless.
	pages := make(chan *vsphere.Inventory, 4)
	fetch := func() tea.Msg {
		defer close(pages)
		partial := func(page *vsphere.Inventory) {
			select {
			case pages <- page:
			default:
			}
		}
		var inv *vsphere.Inventory
		if err := lim.Run(ctx, func() { inv = handle.FetchGroup(group, partial) }); err != nil {
			// Never got to run vsphere.Client.FetchGroup at all — cancelled or
			// timed out waiting for a concurrency slot — so there is no
			// per-kind error of its own to report. Recording it against every
			// kind the group covers matches FetchGroup's own convention: a
			// failure that stops a group before it can even try is reported
			// the same way one that runs and fails is.
			inv = &vsphere.Inventory{Context: cc.Name}
			for _, k := range kindsIn(group) {
				inv.Errors = append(inv.Errors, vsphere.InventoryError{Kind: k, Message: err.Error()})
			}
		}
		return groupMsg{context: cc.Name, cc: cc, generation: generation, group: group, inv: inv}
	}
	return tea.Batch(fetch, listenForPage(cc, generation, group, pages))
}

// listenForPage waits for the next page of a fetch group. It returns no
// message once the stream closes: the group's own result is what ends the
// load, so a stream that has run dry simply stops asking.
func listenForPage(cc *config.Context, generation uint64, group vsphere.FetchGroup, pages chan *vsphere.Inventory) tea.Cmd {
	return func() tea.Msg {
		page, ok := <-pages
		if !ok {
			return nil
		}
		return groupPageMsg{context: cc.Name, cc: cc, generation: generation, group: group, inv: page, pages: pages}
	}
}

// kindsIn lists every Kind a fetch group populates.
func kindsIn(group vsphere.FetchGroup) []vsphere.Kind {
	var kinds []vsphere.Kind
	for _, k := range vsphere.AllKinds {
		if vsphere.GroupFor(k) == group {
			kinds = append(kinds, k)
		}
	}
	return kinds
}

// diagnose walks the connection stages for one context in the background.
func diagnose(ctx context.Context, b Backend, cc *config.Context) tea.Cmd {
	return func() tea.Msg {
		return diagnosisMsg{context: cc.Name, cc: cc, diagnosis: b.Diagnose(ctx, cc)}
	}
}

// formTestMsg carries the outcome of testing an as-yet-unsaved context.
type formTestMsg struct {
	context   *config.Context
	diagnosis *vsphere.Diagnosis
}

// formSaveMsg carries the outcome of saving the form. result is non-nil even
// on failure, so the form can show what was attempted.
type formSaveMsg struct {
	result *contextops.Result
	err    error
}

// formDeleteMsg carries the outcome of removing a context.
type formDeleteMsg struct {
	name    string
	context *config.Context
	err     error
}

// formDiscoverMsg carries the certificate fetched for a not-yet-pinned
// endpoint.
type formDiscoverMsg struct {
	sha256, sha1, subject string
	notAfter              time.Time
	err                   error
}

func testFormContext(ctx context.Context, b Backend, in contextops.Input) tea.Cmd {
	return func() tea.Msg {
		cc, d := b.TestContext(ctx, in)
		return formTestMsg{context: cc, diagnosis: d}
	}
}

func saveFormContext(ctx context.Context, b Backend, in contextops.Input) tea.Cmd {
	return func() tea.Msg {
		res, err := b.SaveContext(ctx, in, true)
		return formSaveMsg{result: res, err: err}
	}
}

// removeContext deletes a context in the background. Removal is not gated on
// a connection, so unlike the other form commands it needs no test stage.
func removeContext(ctx context.Context, b Backend, name string, alsoCredential bool) tea.Cmd {
	return func() tea.Msg {
		cc, err := b.RemoveContext(ctx, name, alsoCredential)
		return formDeleteMsg{name: name, context: cc, err: err}
	}
}

func discoverThumbprint(ctx context.Context, b Backend, cc *config.Context) tea.Cmd {
	return func() tea.Msg {
		sha256, sha1, subject, notAfter, err := b.DiscoverThumbprint(ctx, cc)
		return formDiscoverMsg{sha256: sha256, sha1: sha1, subject: subject, notAfter: notAfter, err: err}
	}
}
