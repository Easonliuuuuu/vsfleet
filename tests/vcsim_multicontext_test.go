//go:build integration

package tests

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/topology"
)

func addFixtureContexts(t *testing.T, r *runner, fixture *vcsimFixture) {
	t.Helper()
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	for name, endpoint := range fixture.Endpoints {
		r.addNonInteractiveContext(name, endpoint.asVCenter(), "env:VSFLEET_E2E_PASSWORD")
	}
}

func captureVCSIM(t *testing.T, r *runner, historyDB string, failOnPartial bool) (assessment.Run, error) {
	t.Helper()
	args := []string{"--history-db", historyDB, "--all-contexts", "-o", "json", "assessment", "run"}
	if failOnPartial {
		args = append(args, "--fail-on-partial")
	}
	stdout, stderr, err := r.run("", args...)
	if err != nil && !failOnPartial {
		t.Fatalf("assessment capture failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var run assessment.Run
	if decodeErr := json.Unmarshal([]byte(stdout), &run); decodeErr != nil {
		t.Fatalf("decode assessment run: %v\nstdout:\n%s\nstderr:\n%s", decodeErr, stdout, stderr)
	}
	return run, err
}

func vcsimJSON(t *testing.T, r *runner, args ...string) string {
	t.Helper()
	stdout, stderr, err := r.run("", args...)
	if err != nil {
		t.Fatalf("vsfleet %s: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, stdout, stderr)
	}
	return stdout
}

func topologyJSON(t *testing.T, r *runner, historyDB, contextName, direction, kind, name string) topology.Result {
	t.Helper()
	args := []string{"--history-db", historyDB, "-o", "json"}
	if contextName != "" {
		args = append(args, "--context", contextName)
	}
	args = append(args, direction, kind, name, "latest")
	stdout := vcsimJSON(t, r, args...)
	var result topology.Result
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode %s JSON: %v\n%s", direction, err, stdout)
	}
	return result
}

// TestVCSIMMultiContextInventoryAndProvenance proves that one assessment run
// spans independent processes and that identical generated names retain
// context provenance in both live and offline command paths.
func TestVCSIMMultiContextInventoryAndProvenance(t *testing.T) {
	fixture := fixtureBasicMultivcenter(t)
	r := newRunner(t)
	addFixtureContexts(t, r, fixture)
	historyDB := filepath.Join(t.TempDir(), "history.db")
	run, _ := captureVCSIM(t, r, historyDB, false)
	if run.Status != assessment.RunComplete || run.RequestedContexts != 2 || run.SuccessfulContexts != 2 {
		t.Fatalf("capture=%+v, want complete two-context run", run)
	}

	var search struct {
		Matches []struct {
			Context string `json:"context"`
			Name    string `json:"name"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(vcsimJSON(t, r, "--all-contexts", "-o", "json", "search", "DC0_C0_RP0_VM0")), &search); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, match := range search.Matches {
		if match.Name == "DC0_C0_RP0_VM0" {
			seen[match.Context] = true
		}
	}
	if !seen["vc-prod"] || !seen["vc-edge"] {
		t.Fatalf("live search provenance=%v, matches=%+v", seen, search.Matches)
	}

	result := topologyJSON(t, r, historyDB, "", "topology", "vm", "DC0_C0_RP0_VM0")
	if len(result.Subjects) != 2 {
		t.Fatalf("offline topology subjects=%d, want two independent names: %+v", len(result.Subjects), result)
	}
	contexts := map[string]bool{}
	for _, subject := range result.Subjects {
		for _, member := range subject.Subject.Members {
			contexts[member.Context] = true
		}
	}
	if !contexts["vc-prod"] || !contexts["vc-edge"] {
		t.Fatalf("offline topology provenance=%v", contexts)
	}

	report := vcsimJSON(t, r, "--history-db", historyDB, "-o", "json", "assessment", "report", strconv.FormatInt(run.ID, 10))
	if !strings.Contains(report, `"vm_count":`) || !strings.Contains(report, `"context": "vc-prod"`) || !strings.Contains(report, `"context": "vc-edge"`) {
		t.Fatalf("assessment report omitted estate/context inventory: %s", report)
	}
}

// TestVCSIMDuplicateNamesStayDistinct verifies the identity joins and the
// local-datastore context rule using two identical external fixtures.
func TestVCSIMDuplicateNamesStayDistinct(t *testing.T) {
	fixture := fixtureDuplicateNames(t)
	r := newRunner(t)
	addFixtureContexts(t, r, fixture)
	historyDB := filepath.Join(t.TempDir(), "history.db")
	if run, _ := captureVCSIM(t, r, historyDB, false); run.Status != assessment.RunComplete {
		t.Fatalf("duplicate-name capture=%+v", run)
	}

	vmResult := topologyJSON(t, r, historyDB, "", "topology", "vm", "DC0_C0_RP0_VM0")
	if len(vmResult.Subjects) != 2 {
		t.Fatalf("same-named VM subjects=%d, want 2: %+v", len(vmResult.Subjects), vmResult)
	}
	dsResult := topologyJSON(t, r, historyDB, "", "topology", "datastore", "LocalDS_0")
	if len(dsResult.Subjects) != 2 {
		t.Fatalf("same-named local datastore subjects=%d, want 2: %+v", len(dsResult.Subjects), dsResult)
	}
}
