package tests

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vmware/govmomi/simulator"
)

func TestAssessmentTopologyQueriesUseCrossVCenterEvidence(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	left := startVCenter(t, func(m *simulator.Model) { m.Datacenter = 1; m.Machine = 2 })
	right := startVCenter(t, func(m *simulator.Model) { m.Datacenter = 1; m.Machine = 2 })
	r := newRunner(t)
	r.addNonInteractiveContext("left", left, "env:VSFLEET_E2E_PASSWORD")
	r.addNonInteractiveContext("right", right, "env:VSFLEET_E2E_PASSWORD")
	addUnreachableContext(r, "offline")
	historyDB := filepath.Join(t.TempDir(), "history.db")
	if _, _, err := r.run("", "--history-db", historyDB, "assessment", "run", "--all-contexts"); err != nil {
		t.Fatalf("assessment run: %v", err)
	}

	stdout := r.mustRun("", "--history-db", historyDB, "--context", "left", "blast-radius", "datastore", "LocalDS_0", "-o", "json")
	var result struct {
		Subjects []struct {
			Edges []struct {
				From struct {
					Name string `json:"name"`
				} `json:"from"`
			} `json:"edges"`
		} `json:"subjects"`
		Blind []struct {
			Context string `json:"context"`
		} `json:"blind"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid topology JSON: %v\n%s", err, stdout)
	}
	foundVM := false
	for _, subject := range result.Subjects {
		for _, edge := range subject.Edges {
			if strings.Contains(edge.From.Name, "VM") {
				foundVM = true
			}
		}
	}
	if !foundVM {
		t.Fatalf("blast radius did not include a simulator VM:\n%s", stdout)
	}
	foundOffline := false
	for _, blind := range result.Blind {
		if blind.Context == "offline" {
			foundOffline = true
		}
	}
	if !foundOffline {
		t.Fatalf("failed context was absent from topology blind coverage:\n%s", stdout)
	}
}
