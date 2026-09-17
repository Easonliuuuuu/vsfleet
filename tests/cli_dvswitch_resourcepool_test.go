// This file exercises issue #158's distributed switch, distributed
// port-group and resource-pool commands end to end against a simulated
// vCenter: normalized evidence that previously existed only for assessment
// exports is now directly queryable.
package tests

import (
	"strings"
	"testing"

	"github.com/vmware/govmomi/simulator"
)

func TestDVSwitchAndPortGroupListing(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 2
		m.Portgroup = 1
	})
	r := newRunner(t)
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")

	switches := r.mustRun(testPassword+"\n", "dvswitch", "list")
	if !strings.Contains(switches, "DVS0") {
		t.Fatalf("dvswitch list did not show the simulated switch:\n%s", switches)
	}

	groups := r.mustRun(testPassword+"\n", "dvportgroup", "list")
	if !strings.Contains(groups, "DVS0") || !strings.Contains(groups, "DC0_DVPG0") {
		t.Fatalf("dvportgroup list did not show the simulated switch and port group:\n%s", groups)
	}

	narrowed := r.mustRun(testPassword+"\n", "dvportgroup", "list", "--switch", "DVS0")
	if !strings.Contains(narrowed, "DC0_DVPG0") {
		t.Fatalf("--switch DVS0 dropped its own port group:\n%s", narrowed)
	}

	_, _, err := r.run(testPassword+"\n", "dvportgroup", "list", "--switch", "no-such-switch")
	if err == nil {
		t.Fatal("an unknown --switch should fail")
	}
	if !strings.Contains(err.Error(), "no-such-switch") {
		t.Errorf("error should name the switch, got: %v", err)
	}
}

func TestResourcePoolListing(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 2
		m.Pool = 1
	})
	r := newRunner(t)
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")

	out := r.mustRun(testPassword+"\n", "resourcepool", "list")
	if !strings.Contains(out, "Resources") {
		t.Fatalf("resourcepool list did not show the cluster's root pool:\n%s", out)
	}
	if !strings.Contains(out, "yes") {
		t.Errorf("resourcepool list should mark the root pool:\n%s", out)
	}

	filtered := r.mustRun(testPassword+"\n", "resourcepool", "list", "--filter", "RP")
	if !strings.Contains(filtered, "RP") {
		t.Errorf("--filter RP did not narrow to the child pool:\n%s", filtered)
	}
	if strings.Contains(filtered, "Resources\t") {
		t.Errorf("--filter RP should have excluded the unrelated root pool:\n%s", filtered)
	}
}
