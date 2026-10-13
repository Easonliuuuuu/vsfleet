package assessment

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// trendCtx describes one context inside a synthetic assessment run.
type trendCtx struct {
	name     string
	vcenter  string
	vmStatus string // success (default), empty, failed
	vms      []vsphere.VM
	ds       []vsphere.Datastore
	dsStatus string // failed or not recorded for incomplete datastore evidence
}

func saveTrendRun(t *testing.T, s *Store, when time.Time, specs ...trendCtx) Run {
	t.Helper()
	cfg := make([]*config.Context, 0, len(specs))
	for _, sp := range specs {
		cfg = append(cfg, &config.Context{Name: sp.name, Endpoint: "https://" + sp.name})
	}
	run, err := s.StartRunWithMetadata(context.Background(), "test", cfg, when, RunMetadata{InventorySchemaVersion: CurrentInventorySchemaVersion})
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range specs {
		status := sp.vmStatus
		if status == "" {
			status = "success"
		}
		collections := make([]CollectionResult, 0, len(persistedKinds))
		for _, kind := range persistedKinds {
			cr := CollectionResult{Kind: kind, Status: "empty"}
			switch kind {
			case "vm":
				cr.Status = status
				cr.ItemCount = len(sp.vms)
			case "datastore":
				if len(sp.ds) > 0 {
					cr.Status = "success"
					for _, d := range sp.ds {
						payload, marshalErr := json.Marshal(d)
						if marshalErr != nil {
							t.Fatal(marshalErr)
						}
						cr.ItemCount++
						cr.Resources = append(cr.Resources, ResourceObservation{Context: sp.name, VCenterID: sp.vcenter, Kind: "datastore", ID: d.ID, Name: d.Name, Payload: payload})
					}
				}
				if sp.dsStatus != "" {
					cr.Status = sp.dsStatus
				}
			}
			if cr.Status == "not recorded" {
				continue
			}
			collections = append(collections, cr)
		}
		obs := make([]Observation, 0, len(sp.vms))
		for _, vm := range sp.vms {
			obs = append(obs, Observation{Context: sp.name, VCenterID: sp.vcenter, VM: vm})
		}
		res := ContextResult{Name: sp.name, VCenterID: sp.vcenter, Status: status, VMs: obs, Collections: collections}
		if status == "failed" {
			res.Error = "collection failed"
		}
		if err := s.SaveContext(context.Background(), run.ID, res, when.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.FinishRun(context.Background(), run.ID, when.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	return run
}

func newTrendTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func day(n int) time.Time { return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC) }

func TestChurnTrendRejectsUnknownAndBlankContexts(t *testing.T) {
	s := newTrendTestStore(t)
	saveTrendRun(t, s, day(1), trendCtx{name: "prod", vcenter: "vc-1", vms: []vsphere.VM{testVM("app", "vm-1", "iu-1", "bu-1", "esx-1")}})

	if _, err := s.ChurnTrend(context.Background(), TrendOptions{Contexts: []string{"ghost"}}); err == nil || !strings.Contains(err.Error(), "unknown assessment context") {
		t.Fatalf("unknown context error = %v", err)
	}
	// Case-insensitive, whitespace-trimmed match must succeed.
	if _, err := s.ChurnTrend(context.Background(), TrendOptions{Contexts: []string{"  PROD "}}); err != nil {
		t.Fatalf("normalized context selector: %v", err)
	}
	if _, err := s.ChurnTrend(context.Background(), TrendOptions{Contexts: []string{"prod", "  "}}); err == nil || !strings.Contains(err.Error(), "must not be blank") {
		t.Fatalf("blank context error = %v", err)
	}
}

func TestTrendContextAbsentFromEligibleWindowIsError(t *testing.T) {
	s := newTrendTestStore(t)
	saveTrendRun(t, s, day(1), trendCtx{name: "edge", vcenter: "vc-e", vms: []vsphere.VM{testVM("e", "vm-e", "iu-e", "bu-e", "esx-e")}})
	r2 := saveTrendRun(t, s, day(2), trendCtx{name: "prod", vcenter: "vc-1", vms: []vsphere.VM{testVM("a", "vm-a", "iu-a", "bu-a", "esx-1")}})
	saveTrendRun(t, s, day(3), trendCtx{name: "prod", vcenter: "vc-1", vms: []vsphere.VM{testVM("a", "vm-a", "iu-a", "bu-a", "esx-1")}})

	// edge exists in history but not within the --from window.
	_, err := s.ChurnTrend(context.Background(), TrendOptions{FromID: r2.ID, Contexts: []string{"edge"}})
	if err == nil || !strings.Contains(err.Error(), "unknown assessment context") {
		t.Fatalf("absent-from-window error = %v", err)
	}
	// Without the window it resolves.
	if _, err := s.ChurnTrend(context.Background(), TrendOptions{Contexts: []string{"edge"}}); err != nil {
		t.Fatalf("edge in full window: %v", err)
	}
}

func TestChurnTrendExcludesUnrelatedRunsAndComparesConsecutiveContributors(t *testing.T) {
	s := newTrendTestStore(t)
	r1 := saveTrendRun(t, s, day(1), trendCtx{name: "prod", vcenter: "vc-1", vms: []vsphere.VM{
		testVM("a", "vm-a", "iu-a", "bu-a", "esx-1"),
		testVM("b", "vm-b", "iu-b", "bu-b", "esx-1"),
	}})
	saveTrendRun(t, s, day(2), trendCtx{name: "edge", vcenter: "vc-e", vms: []vsphere.VM{testVM("e", "vm-e", "iu-e", "bu-e", "esx-e")}})
	r3 := saveTrendRun(t, s, day(3), trendCtx{name: "prod", vcenter: "vc-1", vms: []vsphere.VM{
		testVM("a", "vm-a", "iu-a", "bu-a", "esx-1"),
		testVM("b", "vm-b", "iu-b", "bu-b", "esx-1"),
		testVM("c", "vm-c", "iu-c", "bu-c", "esx-1"),
	}})

	trend, err := s.ChurnTrend(context.Background(), TrendOptions{Contexts: []string{"prod"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(trend.Points) != 2 || trend.Points[0].Run.ID != r1.ID || trend.Points[1].Run.ID != r3.ID {
		t.Fatalf("points = %+v, want prod runs %d and %d", trend.Points, r1.ID, r3.ID)
	}
	if trend.Window.FromRunID != r1.ID || trend.Window.ToRunID != r3.ID {
		t.Fatalf("window = %+v, want %d..%d", trend.Window, r1.ID, r3.ID)
	}
	if trend.Points[1].VMCount != 3 || trend.Points[1].Appeared != 1 {
		t.Fatalf("consecutive contributors compared wrong: %+v", trend.Points[1])
	}
}

func TestChurnTrendContextFilterAppliedBeforeLimit(t *testing.T) {
	s := newTrendTestStore(t)
	var prodRuns []int64
	for i := 1; i <= 5; i++ {
		if i%2 == 1 {
			r := saveTrendRun(t, s, day(i), trendCtx{name: "prod", vcenter: "vc-1", vms: []vsphere.VM{testVM("a", "vm-a", "iu-a", "bu-a", "esx-1")}})
			prodRuns = append(prodRuns, r.ID)
		} else {
			saveTrendRun(t, s, day(i), trendCtx{name: "edge", vcenter: "vc-e", vms: []vsphere.VM{testVM("e", "vm-e", "iu-e", "bu-e", "esx-e")}})
		}
	}
	trend, err := s.ChurnTrend(context.Background(), TrendOptions{Limit: 2, Contexts: []string{"prod"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(trend.Points) != 2 {
		t.Fatalf("points = %d, want 2 (limit counts contributing runs)", len(trend.Points))
	}
	want := prodRuns[len(prodRuns)-2:]
	if trend.Points[0].Run.ID != want[0] || trend.Points[1].Run.ID != want[1] {
		t.Fatalf("points = %d,%d want %v", trend.Points[0].Run.ID, trend.Points[1].Run.ID, want)
	}
}

func TestSnapshotTrendPointsCarryRunMetadataAndSkipUnrelatedRuns(t *testing.T) {
	s := newTrendTestStore(t)
	vmWithSnap := func() vsphere.VM {
		vm := testVM("a", "vm-a", "iu-a", "bu-a", "esx-1")
		vm.Snapshots = []vsphere.VMSnapshot{{ID: "snap-1", Name: "old", CreateTime: day(1).Add(-240 * time.Hour)}}
		return vm
	}
	r1 := saveTrendRun(t, s, day(1), trendCtx{name: "prod", vcenter: "vc-1", vms: []vsphere.VM{vmWithSnap()}})
	saveTrendRun(t, s, day(2), trendCtx{name: "edge", vcenter: "vc-e", vms: []vsphere.VM{testVM("e", "vm-e", "iu-e", "bu-e", "esx-e")}})
	r3 := saveTrendRun(t, s, day(3), trendCtx{name: "prod", vcenter: "vc-1", vms: []vsphere.VM{vmWithSnap()}})

	trend, err := s.SnapshotTrend(context.Background(), TrendOptions{Contexts: []string{"prod"}}, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(trend.Points) != 2 {
		t.Fatalf("points = %d, want 2", len(trend.Points))
	}
	if trend.Points[0].Run.ID != r1.ID || trend.Points[1].Run.ID != r3.ID {
		t.Fatalf("snapshot points lost run metadata: %+v", trend.Points)
	}
	if trend.Points[0].Total != 1 {
		t.Fatalf("snapshot total = %d, want 1", trend.Points[0].Total)
	}
}

func TestCapacityTrendOmitsRunsWhereOwningContextWasNotRecorded(t *testing.T) {
	s := newTrendTestStore(t)
	prodDS := capacityDatastore("prod", "ds-p", "vc-1", 1000, 500)
	edgeDS := capacityDatastore("edge", "ds-e", "vc-e", 1000, 400)
	edgeDS.Backing = vsphere.DatastoreBacking{Local: true}
	prodDS.Backing = vsphere.DatastoreBacking{Local: true}
	r1 := saveTrendRun(t, s, day(1),
		trendCtx{name: "prod", vcenter: "vc-1", ds: []vsphere.Datastore{prodDS}},
		trendCtx{name: "edge", vcenter: "vc-e", ds: []vsphere.Datastore{edgeDS}},
	)
	r2 := saveTrendRun(t, s, day(2), trendCtx{name: "prod", vcenter: "vc-1", ds: []vsphere.Datastore{prodDS}})

	trend, err := s.CapacityTrend(context.Background(), TrendOptions{}, []string{"datastore"})
	if err != nil {
		t.Fatal(err)
	}
	if len(trend.Series) != 5 {
		t.Fatalf("series=%+v, want estate, two contexts and two resources", trend.Series)
	}
	for _, series := range trend.Series {
		want := []int64{r1.ID, r2.ID}
		if series.Name == "edge" || strings.HasPrefix(series.Name, "edge/") {
			want = want[:1]
		}
		if len(series.Points) != len(want) {
			t.Fatalf("%s %q points=%+v, want runs %v", series.Scope, series.Name, series.Points, want)
		}
		encoded, err := json.Marshal(series)
		if err != nil {
			t.Fatal(err)
		}
		var wire struct {
			Points []struct {
				Run struct {
					ID int64 `json:"id"`
				} `json:"run"`
			} `json:"points"`
		}
		if err := json.Unmarshal(encoded, &wire); err != nil {
			t.Fatal(err)
		}
		for i, point := range series.Points {
			if point.Run.ID != want[i] || wire.Points[i].Run.ID != want[i] {
				t.Errorf("%s %q point %d run=%d JSON run=%d, want %d", series.Scope, series.Name, i, point.Run.ID, wire.Points[i].Run.ID, want[i])
			}
		}
	}
}

func TestCapacityTrendScopedCoverageOnlyReportsSelectedContexts(t *testing.T) {
	for _, status := range []string{"failed", "not recorded"} {
		t.Run(status, func(t *testing.T) {
			s := newTrendTestStore(t)
			prodDS := capacityDatastore("prod", "ds-p", "vc-1", 1000, 500)
			prodDS.Backing = vsphere.DatastoreBacking{Local: true}
			// A VM failure is irrelevant to datastore coverage; the lab datastore
			// failure or missing collection must still be visible when selected.
			saveTrendRun(t, s, day(1),
				trendCtx{name: "prod", vcenter: "vc-1", vmStatus: "failed", ds: []vsphere.Datastore{prodDS}},
				trendCtx{name: "lab", vcenter: "vc-l", vmStatus: "failed", dsStatus: status},
			)
			saveTrendRun(t, s, day(2),
				trendCtx{name: "prod", vcenter: "vc-1", ds: []vsphere.Datastore{prodDS}},
				trendCtx{name: "lab", vcenter: "vc-l", vmStatus: "failed", dsStatus: status},
			)

			allContexts, err := s.CapacityTrend(context.Background(), TrendOptions{IncludePartial: true, Contexts: []string{"prod", "lab"}}, []string{"datastore"})
			if err != nil {
				t.Fatal(err)
			}
			if len(allContexts.Coverage) != 2 || allContexts.Coverage[0].Context != "lab" || allContexts.Coverage[1].Context != "lab" {
				t.Fatalf("all-context coverage=%+v, want lab gap in both runs", allContexts.Coverage)
			}
			trend, err := s.CapacityTrend(context.Background(), TrendOptions{IncludePartial: true, Contexts: []string{"prod"}}, []string{"datastore"})
			if err != nil {
				t.Fatal(err)
			}
			if len(trend.Coverage) != 0 {
				t.Fatalf("coverage leaked unrelated context: %+v", trend.Coverage)
			}
		})
	}
}

func TestCapacityReportScopedValidationAndFiltering(t *testing.T) {
	store := newCapacityTestStore(t)
	contexts := []capacityTestContext{
		{name: "prod", vcenter: "vc-1", endpoint: "https://prod"},
		{name: "dr", vcenter: "vc-2", endpoint: "https://dr"},
	}
	for i := 0; i < 3; i++ {
		prod := capacityDatastore("prod", "ds-prod", "vc-1", 1000, int64(500-i*20))
		prod.Backing = vsphere.DatastoreBacking{Local: true}
		dr := capacityDatastore("dr", "ds-dr", "vc-2", 1000, int64(500-i*20))
		dr.Backing = vsphere.DatastoreBacking{Local: true}
		saveCapacityRun(t, store, time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC), contexts,
			map[string][]vsphere.Datastore{"prod": {prod}, "dr": {dr}}, nil, nil)
	}

	if _, err := store.CapacityReport(context.Background(), TrendOptions{Contexts: []string{"ghost"}}, CapacityThresholds{FreePercent: 10}); err == nil {
		t.Fatal("unknown context did not error")
	}

	report, err := store.CapacityReport(context.Background(), TrendOptions{Contexts: []string{"prod"}}, CapacityThresholds{FreePercent: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, ds := range report.Datastores {
		if ds.Object.Context != "prod" {
			t.Fatalf("scoped report leaked context %q", ds.Object.Context)
		}
	}
	if len(report.Datastores) != 1 {
		t.Fatalf("datastores = %d, want 1", len(report.Datastores))
	}
}

func TestTrendMultipleContextsWithDifferentRunCoverage(t *testing.T) {
	s := newTrendTestStore(t)
	r1 := saveTrendRun(t, s, day(1),
		trendCtx{name: "prod", vcenter: "vc-1", vms: []vsphere.VM{testVM("a", "vm-a", "iu-a", "bu-a", "esx-1")}},
		trendCtx{name: "edge", vcenter: "vc-e", vms: []vsphere.VM{testVM("e", "vm-e", "iu-e", "bu-e", "esx-e")}},
	)
	r2 := saveTrendRun(t, s, day(2), trendCtx{name: "prod", vcenter: "vc-1", vms: []vsphere.VM{testVM("a", "vm-a", "iu-a", "bu-a", "esx-1")}})
	r3 := saveTrendRun(t, s, day(3), trendCtx{name: "edge", vcenter: "vc-e", vms: []vsphere.VM{testVM("e", "vm-e", "iu-e", "bu-e", "esx-e")}})

	trend, err := s.ChurnTrend(context.Background(), TrendOptions{Contexts: []string{"prod", "edge"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(trend.Points) != 3 {
		t.Fatalf("points = %d, want 3 (each run covers one requested context)", len(trend.Points))
	}
	if trend.Window.FromRunID != r1.ID || trend.Window.ToRunID != r3.ID {
		t.Fatalf("window = %+v want %d..%d", trend.Window, r1.ID, r3.ID)
	}
	if trend.Points[1].Run.ID != r2.ID {
		t.Fatalf("second point = %d want %d", trend.Points[1].Run.ID, r2.ID)
	}
}

func TestTrendSuccessfulEmptyCollectionKeepsZeroObservation(t *testing.T) {
	s := newTrendTestStore(t)
	r1 := saveTrendRun(t, s, day(1), trendCtx{name: "prod", vcenter: "vc-1", vmStatus: "empty"})
	trend, err := s.ChurnTrend(context.Background(), TrendOptions{Contexts: []string{"prod"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(trend.Points) != 1 || trend.Points[0].Run.ID != r1.ID || trend.Points[0].VMCount != 0 {
		t.Fatalf("empty successful collection dropped or mis-counted: %+v", trend.Points)
	}
}
