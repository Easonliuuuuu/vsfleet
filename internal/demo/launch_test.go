package demo

import (
	"context"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/health"
	"github.com/easonliuuuuu/vsfleet/internal/tui"
)

// Both demo entry points launch through Launch, so this is where the demo's
// History promise lives: five seeded in-memory runs whose newest carries the
// orphaned-VM and zombie-VMDK evidence the help text advertises.
func TestLaunchSeedsHistoryWithHealthFindings(t *testing.T) {
	_, opts, cleanup, err := Launch(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if !opts.Demo || opts.Current != "prod-vc" || opts.Assessment == nil {
		t.Fatalf("options = %+v, want a prod-vc demo with an assessment service", opts)
	}
	ctx := context.Background()
	runs, err := opts.Assessment.Store.Runs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 5 {
		t.Fatalf("seeded runs = %d, want 5", len(runs))
	}
	runID, err := opts.Assessment.Store.ResolveRun(ctx, "latest")
	if err != nil {
		t.Fatal(err)
	}
	data, err := opts.Assessment.Store.LoadExportData(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	report := health.Evaluate(data, health.Options{Thresholds: health.DefaultThresholds()})
	found := map[string]bool{}
	for _, finding := range report.Findings {
		found[finding.Rule] = true
	}
	if !found["vm-orphaned"] || !found["datastore-zombie-vmdk"] {
		t.Fatalf("demo health findings=%v", found)
	}
	confidence := map[health.Confidence]bool{}
	for _, entry := range health.Orphans(data).Entries {
		confidence[entry.Confidence] = true
	}
	if !confidence[health.ConfidenceVerified] || !confidence[health.ConfidenceOtherContext] {
		t.Fatalf("demo orphan confidence=%v", confidence)
	}
}

func TestLaunchWelcomeHonorsOptOut(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want tui.WelcomeKind
	}{
		{"plays by default", nil, tui.WelcomeDemo},
		{"VSFLEET_NO_WELCOME", map[string]string{"VSFLEET_NO_WELCOME": "1"}, tui.WelcomeNone},
		{"CI", map[string]string{"CI": "true"}, tui.WelcomeNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, opts, cleanup, err := Launch(func(key string) string { return tc.env[key] })
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			if opts.Welcome.Kind != tc.want {
				t.Errorf("welcome=%v, want %v", opts.Welcome.Kind, tc.want)
			}
		})
	}
}
