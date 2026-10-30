package tests

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"
)

// Issue #528: a live listing that VMs contradict reports the gap instead of
// reading as an estate with nothing in it. vcsim enforces no permissions, so
// destroying the switch stands in for hiding it: VM NICs still name its UUID
// and the listing no longer returns it.
func TestGetReportsInventoryVMsUseThatWasNotReturned(t *testing.T) {
	model := func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 1
		m.Portgroup = 1
		m.Machine = 1
	}
	const hidden = "possible permission-hidden inventory"

	t.Run("nothing is hidden", func(t *testing.T) {
		t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
		vc := startVCenter(t, model)
		r := newRunner(t)
		r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")
		for _, kind := range []string{"networks", "dvswitches", "dvportgroups"} {
			_, stderr, err := r.run("", "get", kind)
			if err != nil || strings.Contains(stderr, hidden) {
				t.Errorf("get %s: err = %v, stderr = %q; want no warning", kind, err, stderr)
			}
		}
	})

	t.Run("a deleted port group is not hidden", func(t *testing.T) {
		t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
		vc := startVCenter(t, model)
		deletePortGroup(t, vc, "DC0_DVPG0")
		r := newRunner(t)
		r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")
		if _, stderr, err := r.run("", "get", "networks"); err != nil || strings.Contains(stderr, hidden) {
			t.Fatalf("get networks: err = %v, stderr = %q; want no warning", err, stderr)
		}
	})

	t.Run("VMs use a switch that was not returned", func(t *testing.T) {
		t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
		vc := startVCenter(t, model)
		destroyNetwork(t, vc, "DVS0")
		r := newRunner(t)
		r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")
		for _, kind := range []string{"dvswitches", "dvportgroups"} {
			stdout, stderr, err := r.run("", "get", kind)
			if err != nil {
				t.Fatalf("get %s: %v; the gap is a warning, not a failure", kind, err)
			}
			if !strings.Contains(stderr, hidden) || strings.Contains(stderr, "No resources found.") {
				t.Errorf("get %s: stdout = %q, stderr = %q; want the gap, not an empty answer", kind, stdout, stderr)
			}
		}
	})
}

func destroyNetwork(t *testing.T, vc *vcenter, name string) {
	t.Helper()
	ctx := context.Background()
	client, err := govmomi.NewClient(ctx, mustURL(t, vc.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Login(ctx, url.UserPassword("user", "pass")); err != nil {
		t.Fatal(err)
	}
	finder := find.NewFinder(client.Client, true)
	dc, err := finder.DefaultDatacenter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	finder.SetDatacenter(dc)
	network, err := finder.Network(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	dvs, ok := network.(interface {
		Destroy(context.Context) (*object.Task, error)
	})
	if !ok {
		t.Fatalf("%s is a %T, cannot destroy it", name, network)
	}
	task, err := dvs.Destroy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}
