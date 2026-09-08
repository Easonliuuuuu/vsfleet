//go:build integration

package tests

import (
	"path/filepath"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/topology"
)

// TestVCSIMContextIsolation exercises the #116 regression through all three
// topology directions. A valid selector must resolve only its graph, while a
// missing selector must produce an explicit unknown rather than borrowing a
// same-named object from another vCenter.
func TestVCSIMContextIsolation(t *testing.T) {
	fixture := fixtureDuplicateNames(t)
	r := newRunner(t)
	addFixtureContexts(t, r, fixture)
	historyDB := filepath.Join(t.TempDir(), "history.db")
	if run, _ := captureVCSIM(t, r, historyDB, false); run.Status != assessment.RunComplete {
		t.Fatalf("isolation capture=%+v", run)
	}

	for _, contextName := range []string{"vc-a", "vc-b"} {
		for _, direction := range []string{"topology", "dependencies", "blast-radius"} {
			result := topologyJSON(t, r, historyDB, contextName, direction, "vm", "DC0_C0_RP0_VM0")
			if len(result.Subjects) != 1 || result.Subjects[0].Confidence == "unknown" {
				t.Fatalf("%s/%s did not resolve scoped VM: %+v", contextName, direction, result)
			}
			for _, subject := range result.Subjects {
				for _, member := range subject.Subject.Members {
					if member.Context != contextName {
						t.Fatalf("%s/%s leaked member context %q: %+v", contextName, direction, member.Context, result)
					}
				}
				edges := append(append([]topology.Edge(nil), subject.Ancestors...), subject.Edges...)
				for _, edge := range edges {
					if edge.From.Context != "" && edge.From.Context != contextName || edge.To.Context != "" && edge.To.Context != contextName {
						t.Fatalf("%s/%s leaked edge context: %+v", contextName, direction, result)
					}
				}
			}
		}
	}

	for _, direction := range []string{"topology", "dependencies", "blast-radius"} {
		result := topologyJSON(t, r, historyDB, "does-not-exist", direction, "vm", "DC0_C0_RP0_VM0")
		if len(result.Subjects) != 1 || result.Subjects[0].Confidence != "unknown" {
			t.Fatalf("invalid selector %s result=%+v", direction, result)
		}
		if len(result.Subjects[0].Subject.Members) != 0 || len(result.Subjects[0].Ancestors) != 0 || len(result.Subjects[0].Edges) != 0 {
			t.Fatalf("invalid selector %s crossed graph boundary: %+v", direction, result)
		}
	}
}
