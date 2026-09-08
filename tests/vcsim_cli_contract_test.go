//go:build integration

package tests

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
)

// TestVCSIMCLIJSONAndExitContract exercises the machine-readable envelope and
// the real command-tree 0/2/3 exits used by CI callers.
func TestVCSIMCLIJSONAndExitContract(t *testing.T) {
	fixture := fixturePartialFailure(t)
	r := newRunner(t)
	addFixtureContexts(t, r, fixture)
	addUnreachableContext(r, "dead-port")
	historyDB := filepath.Join(t.TempDir(), "history.db")
	partial, _ := captureVCSIM(t, r, historyDB, false)
	if partial.Status != assessment.RunPartial {
		t.Fatalf("contract setup run=%+v", partial)
	}

	stdout, stderr, err := r.run("", "--history-db", historyDB, "-o", "json", "topology", "vm", "not-present", "latest")
	if err != nil {
		t.Fatalf("unknown topology returned error: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var envelope struct {
		SchemaVersion   int                      `json:"schema_version"`
		RunID           int64                    `json:"run_id"`
		CheckedContexts []string                 `json:"checked_contexts"`
		Blind           []map[string]interface{} `json:"blind"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.SchemaVersion == 0 || envelope.RunID == 0 || len(envelope.CheckedContexts) == 0 || len(envelope.Blind) == 0 {
		t.Fatalf("JSON contract fields missing: %+v\n%s", envelope, stdout)
	}

	_, _, partialErr := r.run("", "--history-db", historyDB, "--all-contexts", "-o", "json", "assessment", "run", "--fail-on-partial")
	if exitCode(partialErr) != 3 {
		t.Fatalf("partial command exit=%d err=%v", exitCode(partialErr), partialErr)
	}
	_, _, policyErr := r.run("", "--history-db", historyDB, "-o", "json", "assessment", "diff", "latest", "latest", "--require-complete")
	if exitCode(policyErr) != 2 {
		t.Fatalf("policy command exit=%d err=%v", exitCode(policyErr), policyErr)
	}
}
