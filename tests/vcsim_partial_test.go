//go:build integration

package tests

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/health"
)

// TestVCSIMPartialCoverageSurvivesProcessLoss verifies healthy evidence,
// partial status, coverage diagnostics and exit-code policy after one
// external endpoint is terminated between captures.
func TestVCSIMPartialCoverageSurvivesProcessLoss(t *testing.T) {
	fixture := fixturePartialFailure(t)
	r := newRunner(t)
	addFixtureContexts(t, r, fixture)
	addUnreachableContext(r, "dead-port")
	historyDB := filepath.Join(t.TempDir(), "history.db")

	first, err := captureVCSIM(t, r, historyDB, false)
	if err != nil || first.Status != assessment.RunPartial {
		t.Fatalf("first partial capture run=%+v err=%v", first, err)
	}
	fixture.Endpoints["killable"].Kill()

	second, err := captureVCSIM(t, r, historyDB, false)
	if err != nil || second.Status != assessment.RunPartial || second.SuccessfulContexts != 1 {
		t.Fatalf("post-kill capture run=%+v err=%v", second, err)
	}

	reportJSON := vcsimJSON(t, r, "--history-db", historyDB, "-o", "json", "assessment", "report", "latest")
	var report struct {
		Run      assessment.Run `json:"run"`
		Coverage []struct {
			Context string `json:"context"`
			Status  string `json:"status"`
			Error   string `json:"error"`
		} `json:"coverage"`
	}
	if err := json.Unmarshal([]byte(reportJSON), &report); err != nil {
		t.Fatal(err)
	}
	if report.Run.Status != assessment.RunPartial {
		t.Fatalf("report status=%s, want partial", report.Run.Status)
	}
	coverageText := reportJSON
	if !strings.Contains(coverageText, "killable") || !strings.Contains(coverageText, "dead-port") {
		t.Fatalf("coverage omitted failed contexts: %s", coverageText)
	}

	findingsJSON := vcsimJSON(t, r, "--history-db", historyDB, "-o", "json", "assessment", "findings", "latest")
	var findings health.Report
	if err := json.Unmarshal([]byte(findingsJSON), &findings); err != nil {
		t.Fatal(err)
	}
	if len(findings.Coverage.BlindContexts) == 0 && findings.Coverage.RulesUnknown == 0 {
		t.Fatalf("partial findings looked clean: %s", findingsJSON)
	}
	readinessJSON := vcsimJSON(t, r, "--history-db", historyDB, "-o", "json", "assessment", "readiness", "latest")
	var readiness health.ReadinessReport
	if err := json.Unmarshal([]byte(readinessJSON), &readiness); err != nil {
		t.Fatal(err)
	}
	if readiness.Verdict == "ready" || len(readiness.Unresolved) == 0 {
		t.Fatalf("partial readiness looked clean: %s", readinessJSON)
	}

	_, partialErr := captureVCSIM(t, r, historyDB, true)
	if exitCode(partialErr) != 3 {
		t.Fatalf("--fail-on-partial exit=%d err=%v", exitCode(partialErr), partialErr)
	}
}
