// This file exercises issue #158's "show" commands end to end: identity and
// provenance render for each first-class kind, JSON stays scriptable, and a
// name that collides across vCenters is reported as ambiguous until
// --context narrows it.
package tests

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vmware/govmomi/simulator"
)

func TestShowCommandsRenderIdentityForEveryKind(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 2
		m.Machine = 2
		m.Datastore = 1
		m.Portgroup = 1
		m.App = 1
	})
	r := newRunner(t)
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")

	for _, tc := range []struct {
		kind string
		args []string
		want []string
	}{
		{"vm", []string{"vm", "show", "DC0_C0_RP0_VM0"}, []string{"Name", "DC0_C0_RP0_VM0", "Context", "lab", "Path", "ID"}},
		{"host", []string{"host", "show", "DC0_C0_H0"}, []string{"Name", "DC0_C0_H0", "Cluster", "DC0_C0"}},
		{"cluster", []string{"cluster", "show", "DC0_C0"}, []string{"Name", "DC0_C0", "Datacenter", "DC0"}},
		{"vapp", []string{"vapp", "show", "DC0_C0_APP0"}, []string{"Name", "DC0_C0_APP0", "Status"}},
		{"datastore", []string{"datastore", "show", "LocalDS_0"}, []string{"Name", "LocalDS_0", "Capacity"}},
		{"network", []string{"network", "show", "VM Network"}, []string{"Name", "VM Network", "Type"}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			out := r.mustRun(testPassword+"\n", tc.args...)
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("%v output missing %q:\n%s", tc.args, want, out)
				}
			}
		})
	}
}

func TestShowJSONIncludesStableIdentity(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 2
		m.Machine = 2
	})
	r := newRunner(t)
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")

	stdout := r.mustRun(testPassword+"\n", "vm", "show", "DC0_C0_RP0_VM0", "-o", "json")
	var vm struct {
		ID           string `json:"id"`
		InstanceUUID string `json:"instance_uuid"`
		Context      string `json:"context"`
		Name         string `json:"name"`
	}
	if err := json.Unmarshal([]byte(stdout), &vm); err != nil {
		t.Fatalf("invalid vm show JSON: %v\n%s", err, stdout)
	}
	if vm.ID == "" || vm.InstanceUUID == "" || vm.Context != "lab" || vm.Name != "DC0_C0_RP0_VM0" {
		t.Fatalf("vm show JSON is missing stable identity: %+v\n%s", vm, stdout)
	}
}

func TestShowReportsNotFound(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, nil)
	r := newRunner(t)
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")

	_, stderr, err := r.run(testPassword+"\n", "vm", "show", "does-not-exist")
	if err == nil {
		t.Fatal("show of an unknown VM should fail")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should name the query, got: %v\nstderr: %s", err, stderr)
	}
}

// TestShowIsAmbiguousAcrossContextsUntilNarrowed covers acceptance criteria
// "show resolves exact identities correctly across duplicate names" and
// "--context narrows ambiguous names": two separately simulated vCenters
// produce the same default VM name, so "vm show" across both must refuse to
// guess, and naming --context must resolve it.
func TestShowIsAmbiguousAcrossContextsUntilNarrowed(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	tune := func(m *simulator.Model) { m.Datacenter = 1; m.Machine = 2 }
	left := startVCenter(t, tune)
	right := startVCenter(t, tune)
	r := newRunner(t)
	r.addNonInteractiveContext("left", left, "env:VSFLEET_E2E_PASSWORD")
	r.addNonInteractiveContext("right", right, "env:VSFLEET_E2E_PASSWORD")

	stdin := strings.Repeat(testPassword+"\n", 4)
	stdout, stderr, err := r.run(stdin, "vm", "show", "DC0_H0_VM0", "--all-contexts")
	if err == nil {
		t.Fatalf("an ambiguous show should fail, got success:\n%s", stdout)
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error should say ambiguous, got: %v", err)
	}
	if !strings.Contains(stderr, "left") || !strings.Contains(stderr, "right") {
		t.Errorf("candidates table should list both contexts:\n%s", stderr)
	}

	narrowed := r.mustRun(stdin, "vm", "show", "DC0_H0_VM0", "--context", "left")
	if !strings.Contains(narrowed, "Context") || !strings.Contains(narrowed, "left") {
		t.Errorf("narrowed show did not resolve to the requested context:\n%s", narrowed)
	}
}
