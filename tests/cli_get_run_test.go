package tests

import (
	"strings"
	"testing"
	"time"
)

// TestGetAndDescribeReadACapture captures a vCenter, then points its context
// at a port nothing listens on: whatever --run still answers came from the
// capture, and a live read fails without falling back to it.
func TestGetAndDescribeReadACapture(t *testing.T) {
	vc := startVCenter(t, nil)
	r := newRunner(t)
	r.addContext("lab", vc)
	pw := testPassword + "\n"

	liveVMs := r.mustRun(pw, "get", "vms", "-o", "name")
	liveHBAs := r.mustRun(pw, "get", "hbas", "-o", "name")
	if strings.Count(liveVMs, "\n") < 2 || liveHBAs == "" {
		t.Fatalf("the simulator should hold several VMs and some HBAs; got\n%s\n%s", liveVMs, liveHBAs)
	}
	r.mustRun(pw, "capture", "run", "--label", "nightly")

	r.mustRun(pw, "config", "set-context", "lab",
		"--endpoint", "https://127.0.0.1:1",
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--tls", "insecure",
		"--no-test",
	)

	_, _, err := r.run(pw, "get", "vms")
	if err == nil || !strings.Contains(err.Error(), "add --run latest") {
		t.Fatalf("live get with vCenter down: err = %v; want a pointer to --run latest", err)
	}

	today := time.Now().Format(time.DateOnly)
	for _, run := range []string{"nightly", "latest", "1", today} {
		if got := r.mustRun("", "get", "vms", "--run", run, "-o", "name"); got != liveVMs {
			t.Errorf("get vms --run %s =\n%s\nwant what the live read listed:\n%s", run, got, liveVMs)
		}
	}
	if got := r.mustRun("", "get", "hbas", "--run", "nightly", "-o", "name"); got != liveHBAs {
		t.Errorf("get hbas --run nightly =\n%s\nwant the captured host config:\n%s", got, liveHBAs)
	}

	if got := r.mustRun("", "get", "vms", "--run", "nightly", "--field-selector", "name=DC0_H0_VM0", "-o", "name"); got != "vm/DC0_H0_VM0\n" {
		t.Errorf("--field-selector on a capture listed %q; want vm/DC0_H0_VM0 only", got)
	}

	if got := r.mustRun("", "get", "all", "DC0_H0_VM0", "--run", "nightly", "-o", "name"); strings.TrimSpace(got) != "vm/DC0_H0_VM0" {
		t.Errorf("get all from a capture = %q; want vm/DC0_H0_VM0", got)
	}

	described := r.mustRun("", "describe", "vm", "DC0_H0_VM0", "--run", "nightly")
	for _, want := range []string{"DC0_H0_VM0", "lab", "Power state"} {
		if !strings.Contains(described, want) {
			t.Errorf("describe --run is missing %q:\n%s", want, described)
		}
	}

	if _, _, err := r.run("", "get", "vms", "--run", "weekly"); err == nil || !strings.Contains(err.Error(), `"weekly" not found`) {
		t.Errorf("--run with an unknown label: err = %v; want not found", err)
	}
	if _, _, err := r.run("", "get", "vms", "--run", "nightly", "-n", "prod"); err == nil || !strings.Contains(err.Error(), "-A reads every context it recorded") {
		t.Errorf("--run for a context the capture lacks: err = %v; want a pointer to -A", err)
	}
}
