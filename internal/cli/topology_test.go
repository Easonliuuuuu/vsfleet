package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func newTopologyTestHistoryDB(t *testing.T) string {
	t.Helper()
	var results []assessment.ContextResult
	for _, item := range []struct {
		name, vcenter, id, extent string
	}{
		{name: "prod", vcenter: "vc-prod", id: "ds-prod", extent: "naa.prod"},
		{name: "edge", vcenter: "vc-edge", id: "ds-edge", extent: "naa.edge"},
	} {
		ds := vsphere.Datastore{Location: vsphere.Location{Context: item.name, Datacenter: "dc"}, ID: item.id, Name: "datastore1", Backing: vsphere.DatastoreBacking{Extents: []string{item.extent}}}
		results = append(results, assessment.ContextResult{Name: item.name, VCenterID: item.vcenter, Status: "success", Collections: collections(
			assessment.CollectionResult{Kind: "datastore", Status: "success", ItemCount: 1, Resources: []assessment.ResourceObservation{resourcePayload(t, item.name, item.vcenter, "datastore", item.id, "datastore1", ds)}},
		)})
	}
	return seedHistory(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), "12", results...)
}

func TestTopologyCommandAmbiguityAndContextNarrowing(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	stdout, _, err := runCLI(t, dbPath, "topology", "datastore", "datastore1")
	if err != nil || !strings.Contains(stdout, "[AMBIGUOUS] 2 distinct datastores") || !strings.Contains(stdout, "prod") || !strings.Contains(stdout, "edge") {
		t.Fatalf("ambiguity err=%v output=%s", err, stdout)
	}
	stdout, _, err = runCLI(t, dbPath, "--context", "prod", "topology", "datastore", "datastore1", "-o", "json")
	if err != nil || strings.Contains(stdout, `"ambiguous": true`) {
		t.Fatalf("narrowed err=%v output=%s", err, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil || len(result["subjects"].([]any)) != 1 {
		t.Fatalf("narrowed JSON err=%v output=%s", err, stdout)
	}
}

func TestTopologyCommandRejectsBadKind(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	_, _, err := runCLI(t, dbPath, "topology", "widget", "anything")
	if err == nil || !strings.Contains(err.Error(), `unknown kind "widget"`) {
		t.Fatalf("bad kind error=%v", err)
	}
}
