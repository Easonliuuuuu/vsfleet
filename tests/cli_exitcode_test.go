// This file pins the exit-code contract a scheduled collection depends on. A
// cron job, a systemd timer or a CI step can only react to a partial estate if
// "some vCenters answered" is distinguishable from "all of them did", and that
// distinction has to be stable across releases.
package tests

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// addUnreachableContext registers a context pointing at a closed port, so a
// capture spanning it is partial rather than complete.
func addUnreachableContext(r *runner, name string) {
	r.t.Helper()
	r.mustRun("", "context", "add",
		"--name", name,
		"--endpoint", "https://127.0.0.1:1",
		"--username", "operator@vsphere.local",
		"--credential", "env:VSFLEET_E2E_PASSWORD",
		"--tls", "insecure",
		"--no-test",
	)
}

// exitCode reports the code Execute would return for err: whatever the error
// names, 1 for any other failure, 0 for none.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return 1
}

// A partial capture keeps exiting 0 unless the operator asks otherwise. Every
// scheduled job written before --fail-on-partial existed treats one site being
// down as routine, and an upgrade must not start failing those.
func TestPartialCaptureExitsZeroByDefault(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, nil)
	r := newRunner(t)
	historyDB := filepath.Join(t.TempDir(), "history.db")

	r.addNonInteractiveContext("up", vc, "env:VSFLEET_E2E_PASSWORD")
	// A second context nothing is listening on: the capture stores real
	// evidence from "up" and records "down" as failed.
	addUnreachableContext(r, "down")

	stdout, _, err := r.run("", "--history-db", historyDB, "assessment", "run", "--all-contexts")
	if code := exitCode(err); code != 0 {
		t.Fatalf("a partial capture exited %d without --fail-on-partial (err %v)\n%s", code, err, stdout)
	}
	if !strings.Contains(stdout, "partial") {
		t.Fatalf("the capture was not reported as partial:\n%s", stdout)
	}
}

// With the flag, a partial capture is exit 3 — distinct from 1 (vsfleet could
// not do its job) and from 2 (a policy or health gate failed).
func TestPartialCaptureExitsThreeWhenAsked(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, nil)
	r := newRunner(t)
	historyDB := filepath.Join(t.TempDir(), "history.db")

	r.addNonInteractiveContext("up", vc, "env:VSFLEET_E2E_PASSWORD")
	addUnreachableContext(r, "down")

	stdout, _, err := r.run("", "--history-db", historyDB, "assessment", "run", "--all-contexts", "--fail-on-partial")
	if code := exitCode(err); code != 3 {
		t.Fatalf("a partial capture exited %d with --fail-on-partial (err %v)\n%s", code, err, stdout)
	}
	// The failure has to say how much of the estate answered, or an operator
	// reading a job log cannot tell a one-site outage from a total one.
	if err == nil || !strings.Contains(err.Error(), "1 of 2 contexts") {
		t.Errorf("the error does not say how much of the estate answered: %v", err)
	}
	// The evidence that was collected is still stored: a partial run is a
	// valid run, and the exit code reports it rather than discarding it.
	if list := r.mustRun("", "--history-db", historyDB, "assessment", "list"); !strings.Contains(list, "partial") {
		t.Errorf("the partial run was not stored:\n%s", list)
	}
}

// A capture where everything answered exits 0 even under the flag.
func TestCompleteCaptureExitsZeroUnderFailOnPartial(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, nil)
	r := newRunner(t)
	historyDB := filepath.Join(t.TempDir(), "history.db")

	r.addNonInteractiveContext("up", vc, "env:VSFLEET_E2E_PASSWORD")

	stdout, _, err := r.run("", "--history-db", historyDB, "assessment", "run", "--all-contexts", "--fail-on-partial")
	if code := exitCode(err); code != 0 {
		t.Fatalf("a complete capture exited %d under --fail-on-partial (err %v)\n%s", code, err, stdout)
	}
}
