package assessment

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/perf"
)

// Issue #547: contexts that reach one vCenter share its performance windows,
// so a capture's summary must say which context collected the window it
// shows and where that window falls against the capture.
func TestPerformanceSummaryPlacesTheWindowAgainstTheCapture(t *testing.T) {
	captured := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	type window struct {
		context string
		end     time.Time
	}
	for _, tc := range []struct {
		name    string
		windows []window
		want    PerfSummary
	}{
		{
			name:    "another context's window collected just after the capture",
			windows: []window{{"ctx-b", captured.Add(time.Minute)}},
			want:    PerfSummary{CollectedBy: "ctx-b", Timing: "covers"},
		},
		{
			name:    "the capture's own window is preferred over a newer one covering it",
			windows: []window{{"ctx-a", captured.Add(time.Minute)}, {"ctx-b", captured.Add(2 * time.Minute)}},
			want:    PerfSummary{CollectedBy: "ctx-a", Timing: "covers"},
		},
		{
			name:    "a covering window is preferred over a newer one that starts after the capture",
			windows: []window{{"ctx-b", captured.Add(time.Hour)}, {"ctx-b", captured.Add(48 * time.Hour)}},
			want:    PerfSummary{CollectedBy: "ctx-b", Timing: "covers", WindowEnd: captured.Add(time.Hour)},
		},
		{
			name:    "only a window that starts after the capture",
			windows: []window{{"ctx-a", captured.Add(48 * time.Hour)}},
			want:    PerfSummary{CollectedBy: "ctx-a", Timing: "after"},
		},
		{
			name:    "only a window that ends before the capture",
			windows: []window{{"ctx-a", captured.Add(-time.Hour)}},
			want:    PerfSummary{CollectedBy: "ctx-a", Timing: "before"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx := context.Background()
			r, err := s.StartRun(ctx, "test", []*config.Context{testContext("ctx-a")}, captured)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SaveContext(ctx, r.ID, ContextResult{Name: "ctx-a", VCenterID: "vc-1", Status: "empty"}, captured.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if _, err := s.FinishRun(ctx, r.ID, captured.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			for _, w := range tc.windows {
				if _, err := s.SavePerfWindow(ctx, testPerfWindow(w.context, "vc-1", w.end, perf.WindowPartial)); err != nil {
					t.Fatal(err)
				}
			}
			rows, err := s.PerformanceSummary(ctx, r.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Fatalf("rows = %+v", rows)
			}
			got := PerfSummary{CollectedBy: rows[0].CollectedBy, Timing: rows[0].Timing}
			if !tc.want.WindowEnd.IsZero() {
				got.WindowEnd = rows[0].WindowEnd
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("summary = %+v, want %+v", got, tc.want)
			}
			// testPerfWindow samples one VM and leaves the other unsampled.
			if want := map[string]int{string(perf.SignalPeaksObserved): 1, NotSampled: 1}; !reflect.DeepEqual(rows[0].Signals, want) {
				t.Fatalf("signals = %v, want %v", rows[0].Signals, want)
			}
		})
	}
}
