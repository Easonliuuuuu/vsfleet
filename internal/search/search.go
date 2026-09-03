// Package search queries every configured vCenter at once and merges the
// results. It is the reason this tool exists rather than a per-vCenter script:
// "where is that Ubuntu template" is a question about the whole estate.
package search

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/session"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// Result is one matching inventory object.
type Result struct {
	Context     string
	Kind        vsphere.Kind
	Name        string
	Datacenter  string
	Path        string
	Description string
}

// ContextError records that one vCenter could not be searched. Results from
// the others are still returned: a customer environment behind a dead proxy
// must not blank the whole answer.
type ContextError struct {
	Context string
	Err     error
}

// Results is a whole search outcome, successes and failures together.
type Results struct {
	Query    string
	Matches  []Result
	Failures []ContextError
	Elapsed  time.Duration
	// Searched is how many vCenters answered.
	Searched int
}

// Options tune a search.
type Options struct {
	// Kinds restricts the search. Empty means every kind.
	Kinds []vsphere.Kind
	// Limit caps the number of matches returned, 0 for no limit.
	Limit int
	// Timeout bounds each vCenter individually.
	Timeout time.Duration
	// Concurrency bounds how many vCenters are queried at once.
	Concurrency int
}

func (o Options) wants(k vsphere.Kind) bool {
	if len(o.Kinds) == 0 {
		return true
	}
	for _, want := range o.Kinds {
		if want == k {
			return true
		}
	}
	return false
}

// Search queries every context concurrently and returns the merged matches.
// Matching is a case-insensitive substring test on the object name, which is
// what an operator typing a fragment of a VM name expects.
func Search(ctx context.Context, mgr *session.Manager, contexts []*config.Context, query string, opts Options) *Results {
	start := time.Now()
	needle := strings.ToLower(strings.TrimSpace(query))

	type outcome struct {
		matches []Result
		err     error
	}
	outcomes := make([]outcome, len(contexts))

	limit := opts.Concurrency
	if limit <= 0 {
		limit = session.DefaultConcurrency
	}
	var g errgroup.Group
	g.SetLimit(limit)
	for i, cc := range contexts {
		g.Go(func() error {
			cctx := ctx
			if opts.Timeout > 0 {
				var cancel context.CancelFunc
				cctx, cancel = context.WithTimeout(ctx, opts.Timeout)
				defer cancel()
			}
			matches, err := searchOne(cctx, mgr, cc, needle, opts)
			outcomes[i] = outcome{matches: matches, err: err}
			// Never propagate: one failed vCenter must not cancel the rest.
			return nil
		})
	}
	_ = g.Wait()

	res := &Results{Query: query, Elapsed: time.Since(start)}
	for i, o := range outcomes {
		if o.err != nil {
			res.Failures = append(res.Failures, ContextError{Context: contexts[i].Name, Err: o.err})
			continue
		}
		res.Searched++
		res.Matches = append(res.Matches, o.matches...)
	}
	sort.Slice(res.Matches, func(i, j int) bool {
		a, b := res.Matches[i], res.Matches[j]
		if a.Context != b.Context {
			return a.Context < b.Context
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Name < b.Name
	})
	if opts.Limit > 0 && len(res.Matches) > opts.Limit {
		res.Matches = res.Matches[:opts.Limit]
	}
	return res
}

func searchOne(ctx context.Context, mgr *session.Manager, cc *config.Context, needle string, opts Options) ([]Result, error) {
	tracker := &vsphere.StageTracker{}
	ctx = vsphere.WithStageReporter(ctx, tracker.Report)
	s, err := mgr.Connect(ctx, cc)
	if err != nil {
		return nil, mgr.TimeoutError(err, tracker)
	}
	client := s.Client()
	if client == nil {
		return nil, fmt.Errorf("context %q is not connected", cc.Name)
	}
	inv, err := client.ListInventory(ctx)
	if err != nil {
		return nil, mgr.TimeoutError(err, tracker)
	}
	return Match(inv, needle, opts), nil
}

// Match filters an already-fetched inventory. It is separated from retrieval so
// that a cached inventory can be searched without touching the network.
func Match(inv *vsphere.Inventory, needle string, opts Options) []Result {
	var out []Result
	add := func(kind vsphere.Kind, name, dc, path, desc string) {
		if !opts.wants(kind) {
			return
		}
		if needle != "" && !strings.Contains(strings.ToLower(name), needle) {
			return
		}
		out = append(out, Result{
			Context:     inv.Context,
			Kind:        kind,
			Name:        name,
			Datacenter:  dc,
			Path:        path,
			Description: desc,
		})
	}
	for _, vm := range inv.VMs {
		add(vsphere.KindVM, vm.Name, vm.Datacenter, vm.Path, vm.PowerState)
	}
	for _, vm := range inv.Templates {
		add(vsphere.KindTemplate, vm.Name, vm.Datacenter, vm.Path, vm.GuestOS)
	}
	for _, h := range inv.Hosts {
		add(vsphere.KindHost, h.Name, h.Datacenter, h.Path, h.ConnectionState)
	}
	for _, c := range inv.Clusters {
		add(vsphere.KindCluster, c.Name, c.Datacenter, c.Path, pluralHosts(c.Hosts))
	}
	for _, v := range inv.VApps {
		add(vsphere.KindVApp, v.Name, v.Datacenter, v.Path, v.Status)
	}
	for _, d := range inv.Datastores {
		add(vsphere.KindDatastore, d.Name, d.Datacenter, d.Path, d.Type)
	}
	for _, n := range inv.Networks {
		add(vsphere.KindNetwork, n.Name, n.Datacenter, n.Path, n.Type)
	}
	return out
}

func pluralHosts(n int) string {
	if n == 1 {
		return "1 host"
	}
	return strconv.Itoa(n) + " hosts"
}
