package health

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestDatastoreSpaceLowTiers(t *testing.T) {
	cases := []struct {
		name       string
		free       int64
		thresholds Thresholds
		want       Severity
		expected   string
	}{
		{"above the warning level", 15, DefaultThresholds(), "", ""},
		{"below the warning level", 14, DefaultThresholds(), SeverityWarning, "15"},
		{"at the critical floor", 10, DefaultThresholds(), SeverityWarning, "15"},
		{"below the critical floor", 9, DefaultThresholds(), SeverityCritical, "10"},
		{"below the bytes floor only", 14, Thresholds{DatastoreWarnFreePct: 15, DatastoreFreePct: 10, DatastoreFreeBytes: 20}, SeverityCritical, "10"},
		{"warning level off", 14, Thresholds{DatastoreFreePct: 10}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := healthFixture("5", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
			data.Resources[1].Payload, _ = json.Marshal(vsphere.Datastore{ID: "ds-2", Name: "broken", Accessible: true, CapacityBytes: 100, FreeBytes: 50})
			data.Resources[3].Payload, _ = json.Marshal(vsphere.Datastore{ID: "ds-1", Name: "slow", Accessible: true, CapacityBytes: 100, FreeBytes: tc.free})
			var got []Finding
			for _, f := range Evaluate(data, Options{Thresholds: tc.thresholds}).Findings {
				if f.Rule == "datastore-space-low" {
					got = append(got, f)
				}
			}
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("findings=%+v, want none", got)
				}
				return
			}
			if len(got) != 1 || got[0].Severity != tc.want || got[0].Evidence[0].Field != "free_percent" || got[0].Evidence[0].Expected != tc.expected {
				t.Fatalf("findings=%+v, want one %s finding expecting %s%%", got, tc.want, tc.expected)
			}
		})
	}
}
