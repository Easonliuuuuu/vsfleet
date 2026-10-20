package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/muesli/termenv"
)

// partialEstateModel is History over three partial runs of three vCenters:
// dr fails every time, and the newest run adds 40 app VMs to prod, so the
// default span groups 40 appeared VMs into one row.
func partialEstateModel(t *testing.T, opts Options, width, height int) *Model {
	t.Helper()
	store, err := assessment.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	contexts := []*config.Context{
		{Name: "prod", Endpoint: "https://prod", Username: "user"},
		{Name: "edge-vc", Endpoint: "https://edge", Username: "user"},
		{Name: "dr", Endpoint: "https://dr", Username: "user"},
	}
	var apps []assessment.Observation
	for i := range 40 {
		apps = append(apps, vmObservation(fmt.Sprintf("app-%02d", i), 2, 4096))
	}
	when := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	for i := range 3 {
		run, err := store.StartRun(ctx, "test", contexts, when)
		if err != nil {
			t.Fatal(err)
		}
		var vms []assessment.Observation
		if i == 2 {
			vms = apps
		}
		snapshots := []assessment.CollectionResult{{Kind: "snapshot", Status: "success"}}
		for _, r := range []assessment.ContextResult{
			{Name: "prod", VCenterID: "vc-1", Status: "success", VMs: vms, Collections: snapshots},
			{Name: "edge-vc", VCenterID: "vc-edge", Status: "success", Collections: snapshots},
			{Name: "dr", VCenterID: "vc-dr", Status: "failed", Error: "connection refused"},
		} {
			if err := store.SaveContext(ctx, run.ID, r, when); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.FinishRun(ctx, run.ID, when); err != nil {
			t.Fatal(err)
		}
		when = when.Add(time.Hour)
	}
	opts.Assessment = &assessment.Service{Store: store}
	m := newTestModel(t, twoHealthy(), opts)
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	press(t, m, "H")
	if m.changeDiff == nil {
		t.Fatalf("no diff loaded: %v", m.historyErr)
	}
	return m
}

// TestChangesHeaderKeepsTheListOnShortTerminals checks that the header gives
// up bands on a short terminal so the change list keeps eight rows, while the
// counts stay (issue #477).
func TestChangesHeaderKeepsTheListOnShortTerminals(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {80, 20}, {60, 20}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := partialEstateModel(t, Options{AllContexts: true}, size[0], size[1])
			if got := m.changesListHeight(); got < 8 {
				t.Fatalf("change list has %d rows, want at least 8:\n%s", got, viewText(m))
			}
			view := viewText(m)
			for _, want := range []string{"appeared", "IMPACT"} {
				if !strings.Contains(view, want) {
					t.Fatalf("short header lost %q:\n%s", want, view)
				}
			}
		})
	}
}

// TestChangesCountsNameTheirScope checks that the counts say which vCenters
// they count, since the axis and coverage above describe every vCenter
// (issue #477).
func TestChangesCountsNameTheirScope(t *testing.T) {
	m := partialEstateModel(t, Options{Current: "prod"}, 140, 40)
	for _, line := range strings.Split(viewText(m), "\n") {
		if strings.Contains(line, "appeared") && !strings.Contains(line, "!") {
			if !strings.Contains(line, "prod · ") {
				t.Fatalf("counts do not name their scope: %q", line)
			}
			return
		}
	}
	t.Fatalf("no counts line:\n%s", viewText(m))
}

// TestSelectedRunsStayMarkedWhenPartial checks that the two compared runs
// read differently from the other runs on the axis even when every run is
// partial (issue #477).
func TestSelectedRunsStayMarkedWhenPartial(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	m := partialEstateModel(t, Options{AllContexts: true}, 140, 40)
	m.theme = newTheme()
	ids := m.viewScrubber(m.width, false)[0]
	// style is the escape sequence that opens a run's cell on the axis.
	style := func(id int64) string {
		label := historyRunLabel(id)
		at := strings.Index(ids, label)
		if at < 0 {
			t.Fatalf("axis has no %s: %q", label, ids)
		}
		open := strings.LastIndex(ids[:at], "\x1b[")
		if open < 0 {
			t.Fatalf("%s is not styled: %q", label, ids)
		}
		return ids[open:at]
	}
	unselected := m.runs[len(m.runs)-1].ID // the oldest run is in neither end
	if style(m.targetRun) == style(unselected) || style(m.baseRun) == style(unselected) {
		t.Fatalf("the compared runs are styled like any other partial run: %q", ids)
	}
}

// TestHistoryStatusClearsOnTheNextKey checks that a status line answers only
// the key that set it (issue #477).
func TestHistoryStatusClearsOnTheNextKey(t *testing.T) {
	for _, key := range []string{"down", "1", "esc"} {
		t.Run(key, func(t *testing.T) {
			m := partialEstateModel(t, Options{AllContexts: true}, 140, 40)
			m.setMessage("clipped the span to #1 — the newest run with the same vCenters as #3", false)
			press(t, m, key)
			if m.message != "" {
				t.Fatalf("status %q outlived the next key", m.message)
			}
		})
	}
}

// TestInspectorCountsAndScrollsWhatDoesNotFit checks that a grouped row too
// long for the inspector says how much is cut and that PgDn reaches the end,
// beside the list and full screen (issue #477).
func TestInspectorCountsAndScrollsWhatDoesNotFit(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		keys          []string
	}{
		{"beside the list", 140, 30, nil},
		{"full screen", 80, 24, []string{"enter"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := partialEstateModel(t, Options{AllContexts: true}, tc.width, tc.height)
			press(t, m, tc.keys...)
			view := viewText(m)
			if !strings.Contains(view, "more") || strings.Contains(view, "app-39") {
				t.Fatalf("a clipped inspector does not say how much is cut:\n%s", view)
			}
			for range 10 {
				press(t, m, "pgdown")
			}
			if view := viewText(m); !strings.Contains(view, "app-39") {
				t.Fatalf("PgDn did not reach the last member:\n%s", view)
			}
		})
	}
}

// TestHistoryHelpOffersEnterOnlyWhereItOpensSomething checks that help lists
// Enter only below the split width, where Enter opens the inspector
// (issue #477).
func TestHistoryHelpOffersEnterOnlyWhereItOpensSomething(t *testing.T) {
	for _, tc := range []struct {
		width int
		want  bool
	}{{140, false}, {80, true}} {
		t.Run(fmt.Sprint(tc.width), func(t *testing.T) {
			m := partialEstateModel(t, Options{AllContexts: true}, tc.width, 40)
			press(t, m, "?")
			if got := strings.Contains(viewText(m), "open inspector"); got != tc.want {
				t.Fatalf("help offers enter=%v at %d columns, want %v:\n%s", got, tc.width, tc.want, viewText(m))
			}
		})
	}
}
