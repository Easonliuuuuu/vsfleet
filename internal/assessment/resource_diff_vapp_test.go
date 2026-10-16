package assessment

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestDiffReportsVAppChanges(t *testing.T) {
	web := vsphere.VApp{ID: "resgroup-v1", Name: "web", Status: "started", DirectVMCount: 2}
	stopped := web
	stopped.Status = "stopped"
	db := vsphere.VApp{ID: "resgroup-v2", Name: "db", Status: "started", DirectVMCount: 1}
	for _, tc := range []struct {
		name           string
		before, after  []vsphere.VApp
		change, vappID string
		field          *FieldChange
	}{
		{"appeared", []vsphere.VApp{web}, []vsphere.VApp{web, db}, "appeared", "resgroup-v2", nil},
		{"vanished", []vsphere.VApp{web, db}, []vsphere.VApp{web}, "vanished", "resgroup-v2", nil},
		{"field changed", []vsphere.VApp{web}, []vsphere.VApp{stopped}, "", "resgroup-v1", &FieldChange{Field: "status", Before: `"started"`, After: `"stopped"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			when := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
			base := saveVAppRun(t, s, when, tc.before)
			target := saveVAppRun(t, s, when.Add(time.Hour), tc.after)

			d, err := s.Diff(context.Background(), base.ID, target.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(d.Resources) != 1 || d.Resources[0].Kind != "vapp" || d.Resources[0].ID != tc.vappID {
				t.Fatalf("resources = %+v, want one change to vapp %s", d.Resources, tc.vappID)
			}
			got := d.Resources[0]
			if tc.change != "" && (len(got.Changes) != 1 || got.Changes[0] != tc.change) {
				t.Errorf("changes = %v, want %s", got.Changes, tc.change)
			}
			if tc.field != nil && (len(got.Fields) != 1 || got.Fields[0] != *tc.field) {
				t.Errorf("fields = %+v, want %+v", got.Fields, *tc.field)
			}
		})
	}
}

// saveVAppRun stores one complete run whose only non-empty collection is
// vapps.
func saveVAppRun(t *testing.T, s *Store, when time.Time, vapps []vsphere.VApp) Run {
	t.Helper()
	ctx := context.Background()
	r, err := s.StartRun(ctx, "test", []*config.Context{testContext("prod")}, when)
	if err != nil {
		t.Fatal(err)
	}
	var resources []ResourceObservation
	for _, v := range vapps {
		payload, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		resources = append(resources, ResourceObservation{VCenterID: "vc-1", Context: "prod", Kind: "vapp", ID: v.ID, Name: v.Name, Payload: payload})
	}
	collections := []CollectionResult{{Kind: "vapp", Status: "success", ItemCount: len(resources), Resources: resources}}
	for _, kind := range []string{"vm", "host", "cluster", "resourcepool", "dvswitch", "datastore", "network", "snapshot"} {
		collections = append(collections, CollectionResult{Kind: kind, Status: "empty"})
	}
	if err := s.SaveContext(ctx, r.ID, ContextResult{Name: "prod", VCenterID: "vc-1", Status: "success", Collections: collections}, when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	run, err := s.FinishRun(ctx, r.ID, when.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return run
}
