package tests

import (
	"strings"
	"testing"

	"github.com/vmware/govmomi/simulator"
)

// When every event read is routine (here the newest one, vcsim's power-on),
// no bare header row is printed: stdout stays empty and stderr says why.
func TestEventsWithOnlyRoutineEventsPrintsNoEmptyTable(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, func(m *simulator.Model) { m.Datacenter = 1; m.Machine = 1 })
	r := newRunner(t)
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")

	stdout, stderr, err := r.run("", "events", "--for", "vm/DC0_H0_VM0", "--limit", "1")
	if err != nil {
		t.Fatalf("events: %v\n%s", err, stderr)
	}
	if strings.TrimSpace(stdout) != "" || !strings.Contains(stderr, "No events found.") || !strings.Contains(stderr, "1 of 1 events read are routine and hidden") {
		t.Fatalf("stdout = %q, stderr = %q; want no table and the hidden-event note", stdout, stderr)
	}
}
