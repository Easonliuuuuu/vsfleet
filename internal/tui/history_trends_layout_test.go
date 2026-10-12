package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
)

// TestTrendsLayout pins what the Trends pane answers first: datastores that
// fill soon and rising stale snapshots lead, a quiet estate says it was
// checked, capacity reads as one row per resource, datastores without growth
// evidence fold into one line, and the subtitle names the runs actually used.
// The grid fills the terminal with bars a blank row apart, and stacks each
// chart under its label when the terminal is too narrow to fit them side by
// side.
func TestTrendsLayout(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	runs := []assessment.Run{
		{ID: 7, StartedAt: time.Date(2026, 9, 4, 10, 30, 0, 0, time.UTC)},
		{ID: 8, StartedAt: time.Date(2026, 9, 6, 10, 30, 0, 0, time.UTC)},
	}
	compute := []assessment.CapacityPoint{
		{Run: runs[0], CPUCapacity: f(10000), CPUUsed: f(4000), CPUUtilization: f(40), MemoryCapacity: f(65536), MemoryUsed: f(32768), MemoryUtilization: f(50)},
		{Run: runs[1], CPUCapacity: f(10000), CPUUsed: f(5000), CPUUtilization: f(50), MemoryCapacity: f(65536), MemoryUsed: f(32768), MemoryUtilization: f(50)},
	}
	storage := []assessment.CapacityPoint{
		{Run: runs[0], StorageCapacity: f(10e12), StorageUsed: f(4e12), StorageUtilization: f(40)},
		{Run: runs[1], StorageCapacity: f(10e12), StorageUsed: f(5e12), StorageUtilization: f(50)},
	}
	filling := func(days float64) assessment.DatastoreCapacity {
		return assessment.DatastoreCapacity{
			Object:          assessment.Object{Name: "HCI-Virtual-SAN-Datastore-1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"},
			UsedGrowthBytes: f(15.5e12), SpanDays: 14, FreePercent: f(6),
			Projection: &assessment.CapacityProjection{DaysRemaining: &days},
		}
	}
	quiet := []assessment.DatastoreCapacity{{Object: assessment.Object{Name: "quiet-1"}}, {Object: assessment.Object{Name: "quiet-2"}}}

	tests := []struct {
		name   string
		width  int
		setup  func(m *Model)
		match  []string
		check  func(t *testing.T, lines []string, width int)
		want   []string
		reject []string
		once   []string
		before [2]string
	}{
		{
			name: "datastore filling soon leads",
			setup: func(m *Model) {
				m.historyCapacityReport = &assessment.CapacityReport{Datastores: append([]assessment.DatastoreCapacity{filling(3)}, quiet...)}
			},
			want:   []string{"NEEDS ATTENTION", "fills in ~3 days", "HCI-Virtual-SAN-", "…", "94%", "+ 2 datastores without enough history"},
			reject: []string{"quiet-1", "4c5d-", "No datastore fills"},
			before: [2]string{"fills in ~3 days", "DATASTORE"},
		},
		{
			name: "quiet estate reads as checked",
			setup: func(m *Model) {
				m.historyCapacityReport = &assessment.CapacityReport{Datastores: []assessment.DatastoreCapacity{filling(120)}}
			},
			want:   []string{"✓ No datastore fills within 30 days", "~120 days"},
			reject: []string{"fills in ~120 days"},
		},
		{
			name: "rising stale snapshots need attention",
			setup: func(m *Model) {
				m.historySnapshots.Points[1].Stale = 5
			},
			want:   []string{"stale snapshots rose from 2 to 5"},
			match:  []string{`2 → 5 +\+3`},
			reject: []string{"No datastore fills"},
		},
		{
			name: "capacity is one row per resource",
			setup: func(m *Model) {
				m.historyCapacity = &assessment.CapacityTrend{Series: []assessment.CapacitySeries{
					{Kind: "host", Scope: "estate", Points: compute},
					{Kind: "cluster", Scope: "estate", Points: compute},
					{Kind: "datastore", Scope: "estate", Points: storage},
				}}
			},
			want:   []string{"5.0GHz / 10.0GHz", "+10 pts", "32.0G / 64.0G", "+931G"},
			reject: []string{"host ", "cluster "},
			once:   []string{"CPU ", "Memory ", "Storage "},
		},
		{
			name:   "subtitle names the runs used",
			setup:  func(m *Model) {},
			want:   []string{"2 complete runs", "(2 days)"},
			match:  []string{`40 → 44 +\+4`},
			reject: []string{"last 30", "#7"},
		},
		{
			name:  "bars fill the width a blank row apart",
			width: 140,
			setup: func(m *Model) {
				m.historyCapacity = &assessment.CapacityTrend{Series: []assessment.CapacitySeries{
					{Kind: "host", Scope: "estate", Points: compute},
					{Kind: "datastore", Scope: "estate", Points: storage},
				}}
			},
			check: func(t *testing.T, lines []string, width int) {
				for i, line := range lines {
					if !strings.HasPrefix(line, "  CPU ") && !strings.HasPrefix(line, "  Memory ") {
						continue
					}
					if !strings.Contains(line, "█") {
						t.Errorf("%q should carry its bar on the same line", line)
					}
					if got := len([]rune(strings.TrimRight(line, " "))); got != width-2 {
						t.Errorf("%q ends at column %d, want the right edge %d", line, got, width-2)
					}
					if i+1 < len(lines) && strings.Contains(lines[i+1], "█") {
						t.Errorf("bar rows touch: %q then %q", line, lines[i+1])
					}
				}
			},
		},
		{
			name:  "narrow terminal stacks the chart under its label",
			width: 60,
			setup: func(m *Model) {
				m.historyCapacity = &assessment.CapacityTrend{Series: []assessment.CapacitySeries{{Kind: "host", Scope: "estate", Points: compute}}}
			},
			check: func(t *testing.T, lines []string, width int) {
				for i, line := range lines {
					if strings.HasPrefix(line, "  CPU ") {
						if strings.Contains(line, "█") || i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "  █") {
							t.Errorf("CPU bar should sit alone on the next line:\n%s\n%s", line, lines[min(i+1, len(lines)-1)])
						}
						return
					}
				}
				t.Error("no CPU row")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t, twoHealthy(), Options{Current: "prod"})
			m.width, m.height = 100, 60
			if tt.width > 0 {
				m.width = tt.width
			}
			m.historyChurn = &assessment.ChurnTrend{Points: []assessment.ChurnPoint{{Run: runs[0], VMCount: 40}, {Run: runs[1], VMCount: 44, Appeared: 4}}}
			m.historySnapshots = &assessment.SnapshotTrend{Points: []assessment.SnapshotTrendPoint{{Run: runs[0], Stale: 2}, {Run: runs[1], Stale: 2}}}
			tt.setup(m)

			view := ansi.Strip(strings.Join(m.viewHistoryTrends(), "\n"))
			for _, want := range tt.want {
				if !strings.Contains(view, want) {
					t.Errorf("missing %q", want)
				}
			}
			for _, reject := range tt.reject {
				if strings.Contains(view, reject) {
					t.Errorf("unexpected %q", reject)
				}
			}
			for _, pattern := range tt.match {
				if !regexp.MustCompile(pattern).MatchString(view) {
					t.Errorf("no match for %q", pattern)
				}
			}
			if tt.check != nil {
				tt.check(t, strings.Split(view, "\n"), m.width)
			}
			for _, once := range tt.once {
				if n := strings.Count(view, once); n != 1 {
					t.Errorf("%q appears %d times, want once", once, n)
				}
			}
			if a, b := tt.before[0], tt.before[1]; a != "" && strings.Index(view, a) > strings.Index(view, b) {
				t.Errorf("%q should come before %q", a, b)
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > m.width {
					t.Errorf("line wider than %d: %q", m.width, line)
				}
			}
			if t.Failed() {
				t.Logf("view:\n%s", view)
			}
		})
	}
}
