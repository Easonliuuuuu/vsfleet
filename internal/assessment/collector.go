package assessment

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/session"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

type Collector struct {
	Store   *Store
	Manager *session.Manager
}

// Service is the shared historical API consumed by both front ends.
type Service struct {
	Store     *Store
	Collector *Collector
}

func (s *Service) Runs(ctx context.Context) ([]Run, error) { return s.Store.Runs(ctx) }
func (s *Service) Diff(ctx context.Context, base, target int64, runtime bool) (Diff, error) {
	return s.Store.Diff(ctx, base, target, runtime)
}
func (s *Service) SnapshotAges(ctx context.Context, run int64, older time.Duration) ([]SnapshotAge, error) {
	return s.Store.SnapshotAges(ctx, run, older)
}
func (s *Service) DeleteRun(ctx context.Context, id int64) error { return s.Store.DeleteRun(ctx, id) }
func (s *Service) History(ctx context.Context, query, contextName string) ([]VMHistoryEntry, error) {
	return s.Store.History(ctx, query, contextName)
}
func (s *Service) Capture(ctx context.Context, opts CaptureOptions) (Run, error) {
	if s == nil || s.Collector == nil {
		return Run{}, fmt.Errorf("assessment collector is not configured")
	}
	return s.Collector.Capture(ctx, opts)
}

type CaptureOptions struct {
	Contexts []*config.Context
	Source   string
	Now      func() time.Time
	Progress func(ContextProgress)
}

// Capture creates one immutable run while preserving the session manager's
// partial-success behavior. Every context has its own timeout and commit.
func (c *Collector) Capture(ctx context.Context, opts CaptureOptions) (Run, error) {
	if c.Store == nil || c.Manager == nil {
		return Run{}, fmt.Errorf("assessment collector is not configured")
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	run, err := c.Store.StartRun(ctx, opts.Source, opts.Contexts, now())
	if err != nil {
		return Run{}, err
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, session.DefaultConcurrency)
	for _, cc := range opts.Contexts {
		cc := cc
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if opts.Progress != nil {
				opts.Progress(ContextProgress{Context: cc.Name, Status: "connecting"})
			}
			result := c.captureContext(ctx, cc)
			if err := c.Store.SaveContext(ctx, run.ID, result, now()); err != nil {
				result.Status = "failed"
				result.Error = fmt.Sprintf("save assessment: %v", err)
				_ = c.Store.SaveContext(context.WithoutCancel(ctx), run.ID, result, now())
			}
			if opts.Progress != nil {
				p := ContextProgress{Context: cc.Name, Status: result.Status, VMs: len(result.VMs), Error: errorFrom(result.Error)}
				opts.Progress(p)
			}
		}()
	}
	wg.Wait()
	return c.Store.FinishRun(ctx, run.ID, now())
}

func errorFrom(s string) error {
	if s == "" {
		return nil
	}
	return fmt.Errorf("%s", s)
}

func (c *Collector) captureContext(parent context.Context, cc *config.Context) ContextResult {
	r := ContextResult{Name: cc.Name, Status: "failed"}
	opCtx, cancel, tracker := c.Manager.Operation(parent)
	defer cancel()
	s, err := c.Manager.Connect(opCtx, cc)
	if err != nil {
		r.Error = c.Manager.TimeoutError(err, tracker).Error()
		return r
	}
	client := s.Client()
	if client == nil {
		r.Error = "context is not connected"
		return r
	}
	r.VCenterID = client.About.InstanceID
	// Real vCenters expose About.InstanceUuid. Keep the ledger comparable for
	// compatible endpoints (and deterministic test backends) that omit it by
	// falling back to the configured endpoint rather than an empty identity.
	if r.VCenterID == "" {
		r.VCenterID = cc.Endpoint
	}
	idx, err := client.NewIndex(opCtx)
	if err != nil {
		r.Error = c.Manager.TimeoutError(err, tracker).Error()
		return r
	}
	part := client.FetchGroup(opCtx, idx, vsphere.GroupVMs)
	if msg, failed := part.ErrorFor(vsphere.KindVM); failed {
		r.Error = msg
		return r
	}
	for _, vm := range part.VMs {
		r.VMs = append(r.VMs, Observation{VCenterID: r.VCenterID, Context: cc.Name, VM: vm})
	}
	r.Status = "success"
	if len(r.VMs) == 0 {
		r.Status = "empty"
	}
	return r
}
