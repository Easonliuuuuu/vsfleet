//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
)

func mutateVCSIMVM(t *testing.T, endpoint *simEndpoint, vmName string, powerOff bool, rename string) {
	t.Helper()
	ctx := context.Background()
	u, err := url.Parse(endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/sdk"
	u.User = url.UserPassword(vcsimUsername, testPassword)
	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		t.Fatalf("connect to vcsim for mutation: %v", err)
	}
	defer client.Logout(ctx)
	finder := find.NewFinder(client.Client, true)
	dc, err := finder.Datacenter(ctx, "DC0")
	if err != nil {
		t.Fatalf("find datacenter: %v", err)
	}
	finder.SetDatacenter(dc)
	vm, err := finder.VirtualMachine(ctx, vmName)
	if err != nil {
		t.Fatalf("find VM %q: %v", vmName, err)
	}
	if powerOff {
		task, err := vm.PowerOff(ctx)
		if err != nil {
			t.Fatalf("power off %q: %v", vmName, err)
		}
		if err := task.Wait(ctx); err != nil {
			t.Fatalf("wait for power off %q: %v", vmName, err)
		}
	}
	if rename != "" {
		task, err := vm.Rename(ctx, rename)
		if err != nil {
			t.Fatalf("rename %q: %v", vmName, err)
		}
		if err := task.Wait(ctx); err != nil {
			t.Fatalf("wait for rename %q: %v", vmName, err)
		}
	}
}

func assertCapacityRunIDs(t *testing.T, raw string, want int) {
	t.Helper()
	var trend assessment.CapacityTrend
	if err := json.Unmarshal([]byte(raw), &trend); err != nil {
		t.Fatal(err)
	}
	if len(trend.Series) == 0 {
		t.Fatalf("capacity trend has no series: %s", raw)
	}
	for _, series := range trend.Series {
		if len(series.Points) != want {
			t.Fatalf("%s/%s/%s points=%d, want %d", series.Kind, series.Scope, series.Name, len(series.Points), want)
		}
		for i, point := range series.Points {
			if point.Run.ID == 0 {
				t.Fatalf("%s/%s/%s point %d lost run ID", series.Kind, series.Scope, series.Name, i)
			}
			if i > 0 && point.Run.ID <= series.Points[i-1].Run.ID {
				t.Fatalf("%s/%s/%s run IDs are not ordered: %+v", series.Kind, series.Scope, series.Name, series.Points)
			}
		}
	}
}

// TestVCSIMHistoryDiffVMHistoryAndCapacity proves that independent vcsim
// captures retain identity, mutations, context-scoped history and real run
// IDs in all capacity trend scopes (#117).
func TestVCSIMHistoryDiffVMHistoryAndCapacity(t *testing.T) {
	fixture := fixtureHistory(t)
	r := newRunner(t)
	addFixtureContexts(t, r, fixture)
	historyDB := filepath.Join(t.TempDir(), "history.db")
	first, _ := captureVCSIM(t, r, historyDB, false)
	mutateVCSIMVM(t, fixture.Endpoints["history"], "DC0_C0_RP0_VM0", true, "")
	second, _ := captureVCSIM(t, r, historyDB, false)
	mutateVCSIMVM(t, fixture.Endpoints["history"], "DC0_C0_RP0_VM1", false, "renamed-vm")
	third, _ := captureVCSIM(t, r, historyDB, false)
	if first.ID == second.ID || second.ID == third.ID {
		t.Fatalf("captures did not create distinct runs: %d, %d, %d", first.ID, second.ID, third.ID)
	}

	diffJSON := vcsimJSON(t, r, "--history-db", historyDB, "-o", "json", "assessment", "diff", strconv.FormatInt(first.ID, 10), strconv.FormatInt(second.ID, 10), "--include-runtime")
	if !strings.Contains(diffJSON, "power_state") && !strings.Contains(diffJSON, "modified") {
		t.Fatalf("diff omitted power mutation: %s", diffJSON)
	}
	historyJSON := vcsimJSON(t, r, "--history-db", historyDB, "-o", "json", "vm", "history", "DC0_C0_RP0_VM0", "--all-observations", "--include-runtime")
	if !strings.Contains(historyJSON, strconv.FormatInt(first.ID, 10)) || !strings.Contains(historyJSON, strconv.FormatInt(second.ID, 10)) {
		t.Fatalf("VM history omitted captures: %s", historyJSON)
	}
	trendJSON := vcsimJSON(t, r, "--history-db", historyDB, "-o", "json", "assessment", "trends", "capacity", "--kind", "all", "--limit", "0")
	assertCapacityRunIDs(t, trendJSON, 3)
	contextTrendJSON := vcsimJSON(t, r, "--history-db", historyDB, "--context", "history", "-o", "json", "assessment", "trends", "capacity", "--kind", "host", "--limit", "0")
	assertCapacityRunIDs(t, contextTrendJSON, 3)
	if !strings.Contains(contextTrendJSON, `"scope": "context"`) || !strings.Contains(contextTrendJSON, `"name": "history"`) {
		t.Fatalf("context-scoped capacity trend omitted context series: %s", contextTrendJSON)
	}
}
