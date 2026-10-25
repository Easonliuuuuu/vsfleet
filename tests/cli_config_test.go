package tests

import (
	"strings"
	"testing"
)

// "config set-context" on an existing context changes only the settings
// passed. The update is tested against the vCenter like a new context, so
// the kept credential reference and pinned thumbprint have to survive for
// the save to succeed at all.
func TestSetContextUpdatesOnlyThePassedSettings(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, nil)
	r := newRunner(t)
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")

	out := r.mustRun("", "config", "set-context", "lab", "--datacenter", "DC0")
	if !strings.Contains(out, `Context "lab" modified`) {
		t.Errorf("set-context did not report a modified context:\n%s", out)
	}

	view := r.mustRun("", "config", "view", "--minify", "-n", "lab")
	for _, want := range []string{vc.URL, "env:VSFLEET_E2E_PASSWORD", vc.Thumbprint, "DC0"} {
		if !strings.Contains(view, want) {
			t.Errorf("config view is missing %q after the update:\n%s", want, view)
		}
	}
}
