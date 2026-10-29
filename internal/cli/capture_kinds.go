package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/perf"
)

// captureLister lists stored captures for "get captures" and "capture list".
// A capture spans contexts, so -n and -A do not narrow it.
func captureLister() kindLister {
	return lister[assessment.Run]{
		history: true,
		fetch: func(ctx context.Context, a *App, _ string) ([]assessment.Run, []contextFailure, error) {
			s, err := a.History()
			if err != nil {
				return nil, nil, err
			}
			runs, err := s.Runs(ctx)
			return runs, nil, err
		},
		names:   func(r assessment.Run) []string { return []string{strconv.FormatInt(r.ID, 10), r.Label} },
		context: func(assessment.Run) string { return "" },
		headers: []string{"ID", "LABEL", "SOURCE", "STARTED", "STATUS", "CONTEXTS", "SUCCESSFUL"},
		row: func(r assessment.Run) []string {
			label := r.Label
			if r.Pinned {
				label = "📌 " + label
			}
			return []string{strconv.FormatInt(r.ID, 10), dash(label), dash(r.Source), r.StartedAt.Local().Format("2006-01-02 15:04:05"),
				string(r.Status), itoa(r.RequestedContexts), itoa(r.SuccessfulContexts)}
		},
	}
}

// perfCollectionLister lists stored performance collections. Each belongs to
// one context, so -n narrows them the way it narrows a stored analysis:
// without it, every context's are listed.
func perfCollectionLister() kindLister {
	return lister[perf.Window]{
		history: true,
		fetch: func(ctx context.Context, a *App, _ string) ([]perf.Window, []contextFailure, error) {
			s, err := a.History()
			if err != nil {
				return nil, nil, err
			}
			windows, err := s.PerfWindows(ctx, "", 1000)
			if err != nil {
				return nil, nil, err
			}
			if names := a.StoredContextNames(); len(names) > 0 {
				windows = filterSlice(windows, func(w perf.Window) bool { return slices.Contains(names, w.Context) })
			}
			return windows, nil, nil
		},
		names:   func(w perf.Window) []string { return []string{strconv.FormatInt(w.ID, 10)} },
		context: func(w perf.Window) string { return w.Context },
		headers: []string{"ID", "CONTEXT", "STATUS", "WINDOW END", "SPAN", "INTERVAL", "VMS", "REQUESTS"},
		row: func(w perf.Window) []string {
			return []string{strconv.FormatInt(w.ID, 10), w.Context, w.Status, w.WindowEnd.Local().Format("2006-01-02 15:04"),
				windowSpan(w.WindowEnd.Sub(w.WindowStart)), fmt.Sprintf("%ds", w.IntervalSeconds),
				fmt.Sprintf("%d/%d", w.VMsSampled, w.VMsRequested), itoa(w.RequestsUsed)}
		},
	}
}

// windowSpan reads a collection window as whole days, or hours when it is
// not a whole number of days: "1d", "7d", "30d", "6h".
func windowSpan(d time.Duration) string {
	hours := int64(d.Round(time.Hour) / time.Hour)
	if hours > 0 && hours%24 == 0 {
		return strconv.FormatInt(hours/24, 10) + "d"
	}
	return strconv.FormatInt(hours, 10) + "h"
}

// captureReport is what "describe capture" shows: the capture's counts and
// coverage, and the performance collected alongside it.
type captureReport struct {
	assessment.AssessmentReport
	Performance []assessment.PerfSummary `json:"performance"`
}

// captureStaleAge is how old a snapshot must be to count as stale in a
// capture's summary; "analyze snapshots --older-than" takes any age.
const captureStaleAge = 30 * 24 * time.Hour

// captureDescriber describes a stored capture. It is read from the history
// database, never live and never through --run, and -n narrows the coverage
// it reports.
type captureDescriber struct{}

func (captureDescriber) resolve(ctx context.Context, a *App, names []string) ([]any, []contextFailure, error) {
	if a.capture != nil {
		return nil, nil, fmt.Errorf(`"describe capture" names the capture itself; --run does not apply`)
	}
	s, err := a.History()
	if err != nil {
		return nil, nil, err
	}
	var found []any
	for _, name := range names {
		id, err := s.ResolveRun(ctx, name)
		if err != nil {
			return found, nil, err
		}
		report, err := s.ReportForContexts(ctx, id, captureStaleAge, a.StoredContextNames())
		if err != nil {
			return found, nil, err
		}
		performance, err := s.PerformanceSummary(ctx, id, a.StoredContextNames())
		if err != nil {
			return found, nil, err
		}
		found = append(found, captureReport{report, performance})
	}
	return found, nil, nil
}

func (captureDescriber) print(out io.Writer, object any) {
	r := object.(captureReport)
	fmt.Fprintf(out, "Capture %d  %s  %s\n", r.Run.ID, r.Run.Status, r.Run.StartedAt.Local().Format("2006-01-02 15:04:05"))
	if r.Run.Label != "" || r.Run.Pinned {
		pinned := ""
		if r.Run.Pinned {
			pinned = "  (pinned)"
		}
		fmt.Fprintf(out, "Label: %s%s\n", dash(r.Run.Label), pinned)
	}
	if r.Run.Note != "" {
		fmt.Fprintf(out, "Note: %s\n", r.Run.Note)
	}
	fmt.Fprintf(out, "VMs: %d  Hosts: %d  Clusters: %d  Datastores: %d  Snapshots: %d (%d older than 30d)\n",
		r.VMCount, r.HostCount, r.ClusterCount, r.DatastoreCount, r.SnapshotTotal, r.SnapshotStale)
	t := newTable(out, "CONTEXT", "KIND", "STATUS", "ITEMS", "ERROR")
	for _, c := range r.Coverage {
		t.row(c.Context, c.Kind, c.Status, itoa(c.ItemCount), c.Error)
	}
	t.flush()
	printPerformanceSummary(out, r.Performance)
	if len(r.Warnings) > 0 {
		fmt.Fprintln(out, "\nWarnings:")
		for _, w := range r.Warnings {
			fmt.Fprintf(out, "  %s %s\n", glyphFail, w)
		}
	}
}
