package vsphere

import (
	"context"
	"sync/atomic"
)

// Stage is one named phase of a per-context operation — resolving a
// credential, connecting, authenticating, or enumerating one resource kind.
// Connect and ListInventory report their progress through it as they reach
// each phase, so a caller can show live status or name what was in flight
// when an operation timed out, without either function knowing anything
// about how — or whether — that is used.
type Stage string

// Stages reported by Connect and ListInventory, in the order a successful
// operation passes through them. Not every operation reaches every stage: a
// failure or a caller-imposed deadline can end things at any one of them.
const (
	StageResolvingCredentials Stage = "resolving credentials"
	StageConnecting           Stage = "connecting"
	StageAuthenticating       Stage = "authenticating to vCenter"
	StageLoadingIndex         Stage = "loading inventory index"
	StageLoadingVMs           Stage = "loading VMs and templates"
	StageLoadingHosts         Stage = "loading hosts"
	StageLoadingClusters      Stage = "loading clusters"
	StageLoadingVApps         Stage = "loading vApps"
	StageLoadingDatastores    Stage = "loading datastores"
	StageLoadingNetworks      Stage = "loading networks"
)

type stageReporterKey struct{}

// WithStageReporter returns a context that Connect and ListInventory report
// their current Stage to as they reach it. report may be called from
// whatever goroutine is doing the work; StageTracker.Report is safe for
// that, and is the reporter every caller in this codebase uses.
//
// A nil report is a no-op: it returns ctx unchanged, so a caller that does
// not care about progress does not need to special-case this.
func WithStageReporter(ctx context.Context, report func(Stage)) context.Context {
	if report == nil {
		return ctx
	}
	return context.WithValue(ctx, stageReporterKey{}, report)
}

// reportStage tells whatever reporter is attached to ctx, if any, that the
// operation has reached s.
func reportStage(ctx context.Context, s Stage) {
	if report, ok := ctx.Value(stageReporterKey{}).(func(Stage)); ok {
		report(s)
	}
}

// StageTracker holds the most recently reported Stage for one operation. Its
// zero value is ready to use. Report is the function to pass to
// WithStageReporter; Current reads it back and is safe to call from a
// different goroutine — a UI's render loop, or an error path naming the
// stage an operation was in when its context's deadline expired.
type StageTracker struct {
	stage atomic.Pointer[Stage]
}

// Report records s as the current stage.
func (t *StageTracker) Report(s Stage) { t.stage.Store(&s) }

// Current returns the last reported stage, or "" if nothing has been
// reported yet.
func (t *StageTracker) Current() Stage {
	if t == nil {
		return ""
	}
	if s := t.stage.Load(); s != nil {
		return *s
	}
	return ""
}
