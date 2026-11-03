package assessment

import (
	"context"
	"strings"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/perf"
)

// PerfSummary is the short per-context performance line shown in an
// assessment report. Status is "not collected" for a context with no usable
// window, so an absent collection is never mistaken for a quiet estate.
type PerfSummary struct {
	Context string `json:"context"`
	Status  string `json:"status"`
	// CollectedBy is the context that collected the window. Contexts that
	// reach the same vCenter share its windows, so it can differ from Context.
	CollectedBy     string    `json:"collected_by,omitempty"`
	WindowID        int64     `json:"window_id,omitempty"`
	WindowStart     time.Time `json:"window_start,omitempty"`
	WindowEnd       time.Time `json:"window_end,omitempty"`
	IntervalSeconds int       `json:"interval_seconds,omitempty"`
	// Timing places the window against the capture: "covers", "after" (it
	// starts after the capture) or "before" (it ends before the capture).
	Timing       string         `json:"timing,omitempty"`
	VMsSampled   int            `json:"vms_sampled,omitempty"`
	VMsRequested int            `json:"vms_requested,omitempty"`
	Signals      map[string]int `json:"signals,omitempty"`
}

// NotSampled is the Signals key for VMs a window requested but never queried.
// Their stored signal is insufficient-data, which would otherwise read as a
// VM that was sampled without enough data.
const NotSampled = "not-sampled"

// PerformanceSummary reports, for each selected context of a run, the usable
// performance window that best describes the capture. It reads only stored
// evidence.
func (s *Store) PerformanceSummary(ctx context.Context, runID int64, selectors []string) ([]PerfSummary, error) {
	contexts, err := s.ContextRuns(ctx, runID)
	if err != nil {
		return nil, err
	}
	selected, err := ValidateStoredContexts(selectors, contexts)
	if err != nil {
		return nil, err
	}
	var out []PerfSummary
	for _, c := range contexts {
		if len(selected) > 0 && !contextSelected(c.Name, selected) {
			continue
		}
		w, timing, found, err := s.capturePerfWindow(ctx, c)
		if err != nil {
			return nil, err
		}
		if !found {
			out = append(out, PerfSummary{Context: c.Name, Status: "not collected"})
			continue
		}
		row := PerfSummary{Context: c.Name, Status: w.Status, CollectedBy: w.Context, WindowID: w.ID, WindowStart: w.WindowStart, WindowEnd: w.WindowEnd,
			IntervalSeconds: w.IntervalSeconds, Timing: timing, VMsSampled: w.VMsSampled, VMsRequested: w.VMsRequested, Signals: map[string]int{}}
		for _, vm := range w.VMs {
			if vm.Sampled {
				row.Signals[string(vm.Signal)]++
			} else {
				row.Signals[NotSampled]++
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// capturePerfWindow picks the window shown beside a capture's context: one
// whose span covers the capture's time, preferring the context's own and then
// the newest, or else the newest window, with timing saying which side of
// the capture it falls on. Windows are matched by vCenter identity where the
// run recorded one, so a renamed context keeps its history.
func (s *Store) capturePerfWindow(ctx context.Context, c ContextRun) (perf.Window, string, bool, error) {
	query := `SELECT ` + perfWindowColumns + ` FROM perf_windows WHERE status IN ('success','partial') AND `
	var arg string
	if c.VCenterID != "" {
		query, arg = query+`vcenter_id=?`, c.VCenterID
	} else {
		query, arg = query+`context=? AND vcenter_id=''`, c.Name
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY window_end DESC, id DESC`, arg)
	if err != nil {
		return perf.Window{}, "", false, err
	}
	var windows []perf.Window
	for rows.Next() {
		w, err := scanPerfWindow(rows)
		if err != nil {
			rows.Close()
			return perf.Window{}, "", false, err
		}
		windows = append(windows, w)
	}
	if err := rows.Close(); err != nil {
		return perf.Window{}, "", false, err
	}
	if err := rows.Err(); err != nil {
		return perf.Window{}, "", false, err
	}
	if len(windows) == 0 {
		return perf.Window{}, "", false, nil
	}
	at := c.StartedAt
	chosen := -1
	for i, w := range windows {
		if windowTiming(w, at) != "covers" {
			continue
		}
		if chosen < 0 || (sameContextName(w.Context, c.Name) && !sameContextName(windows[chosen].Context, c.Name)) {
			chosen = i
		}
	}
	if chosen < 0 {
		chosen = 0
	}
	w := windows[chosen]
	if err := s.loadPerfVMs(ctx, &w); err != nil {
		return perf.Window{}, "", false, err
	}
	return w, windowTiming(w, at), true, nil
}

func windowTiming(w perf.Window, at time.Time) string {
	switch {
	case at.IsZero():
		return ""
	case w.WindowStart.After(at):
		return "after"
	case w.WindowEnd.Before(at):
		return "before"
	}
	return "covers"
}

// sameContextName compares stored context names the way selectors match
// them: case-insensitively, ignoring surrounding whitespace.
func sameContextName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
