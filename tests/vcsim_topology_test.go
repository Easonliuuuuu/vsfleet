//go:build integration

package tests

import (
	"path/filepath"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/topology"
)

// TestVCSIMTopologyAncestryAndAttachments checks the exact deterministic
// hierarchy and VM attachment edges from the small topology fixture.
func TestVCSIMTopologyAncestryAndAttachments(t *testing.T) {
	fixture := fixtureTopology(t)
	r := newRunner(t)
	addFixtureContexts(t, r, fixture)
	historyDB := filepath.Join(t.TempDir(), "history.db")
	if run, _ := captureVCSIM(t, r, historyDB, false); run.Status != assessment.RunComplete {
		t.Fatalf("topology capture=%+v", run)
	}
	result := topologyJSON(t, r, historyDB, "", "topology", "vm", "DC0_C0_RP0_VM0")
	if len(result.Subjects) != 1 {
		t.Fatalf("topology subjects=%+v", result)
	}
	seenHierarchy := map[string]bool{}
	allEdges := append(append([]topology.Edge(nil), result.Subjects[0].Ancestors...), result.Subjects[0].Edges...)
	for _, edge := range allEdges {
		seenHierarchy[edge.From.Kind] = true
		seenHierarchy[edge.To.Kind] = true
		if edge.From.Context != "topology" || edge.To.Context != "topology" {
			t.Fatalf("topology edge escaped context: %+v", edge)
		}
	}
	for _, kind := range []string{"host", "cluster", "datacenter"} {
		if !seenHierarchy[kind] {
			t.Fatalf("missing %s hierarchy evidence: %+v", kind, allEdges)
		}
	}
	seenRelations := map[topology.Relation]bool{}
	for _, edge := range result.Subjects[0].Edges {
		seenRelations[edge.Relation] = true
		if edge.From.Context != "topology" || edge.To.Context != "topology" {
			t.Fatalf("attachment escaped topology context: %+v", edge)
		}
	}
	if !seenRelations[topology.RelationStoredOn] || !seenRelations[topology.RelationAttachedTo] {
		t.Fatalf("missing VM datastore/network attachments: %+v", result.Subjects[0].Edges)
	}
}

// TestVCSIMBlastRadiusNeverCrossesDuplicateNameContexts checks that a
// same-named local datastore in one endpoint cannot traverse into the other.
func TestVCSIMBlastRadiusNeverCrossesDuplicateNameContexts(t *testing.T) {
	fixture := fixtureDuplicateNames(t)
	r := newRunner(t)
	addFixtureContexts(t, r, fixture)
	historyDB := filepath.Join(t.TempDir(), "history.db")
	if run, _ := captureVCSIM(t, r, historyDB, false); run.Status != assessment.RunComplete {
		t.Fatalf("blast-radius capture=%+v", run)
	}
	result := topologyJSON(t, r, historyDB, "vc-a", "blast-radius", "datastore", "LocalDS_0")
	if len(result.Subjects) != 1 || result.Subjects[0].Confidence == topology.ConfidenceUnknown {
		t.Fatalf("scoped datastore blast-radius=%+v", result)
	}
	for _, edge := range result.Subjects[0].Edges {
		if edge.From.Context != "vc-a" || edge.To.Context != "vc-a" {
			t.Fatalf("blast-radius crossed context: %+v", result)
		}
	}
}
