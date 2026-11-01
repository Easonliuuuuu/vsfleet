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
	"github.com/vmware/govmomi/vim25/types"
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
		for _, kind := range []string{"networks", "dvswitches", "dvportgroups", "hosts", "pnics"} {
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

// Issue #533: a live host listing is checked as a capture's is. vcsim enforces
// no permissions, so each case edits what the hidden host would leave behind.
func TestGetReportsHostsTheInventoryCountsThatWereNotReturned(t *testing.T) {
	const hidden = "possible permission-hidden inventory: "
	for _, tc := range []struct {
		name   string
		hide   func(*simulator.Registry)
		reason string
	}{
		{
			name: "a cluster counts more hosts than were returned",
			hide: func(m *simulator.Registry) {
				cluster := m.Any("ClusterComputeResource").(*simulator.ClusterComputeResource)
				cluster.Summary.GetComputeResourceSummary().NumHosts++
			},
			reason: `cluster "DC0_C0" has 2 host(s) but only 1 were returned`,
		},
		{
			name: "a powered-on VM runs on a host that was not returned",
			hide: func(m *simulator.Registry) {
				vm := m.Any("VirtualMachine").(*simulator.VirtualMachine)
				vm.Runtime.Host = &types.ManagedObjectReference{Type: "HostSystem", Value: "host-hidden"}
			},
			reason: "reports no host",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
			vc := startVCenter(t, func(m *simulator.Model) {
				m.Datacenter = 1
				m.Host = 0
				m.Cluster = 1
				m.ClusterHost = 1
				m.Machine = 1
			})
			tc.hide(vc.Service.Context.Map)
			r := newRunner(t)
			r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")
			for _, kind := range []string{"hosts", "pnics"} {
				stdout, stderr, err := r.run("", "get", kind)
				if err != nil {
					t.Fatalf("get %s: %v; the gap is a warning, not a failure", kind, err)
				}
				if !strings.Contains(stderr, hidden) || !strings.Contains(stderr, tc.reason) {
					t.Errorf("get %s: stderr = %q; want %q", kind, stderr, tc.reason)
				}
				if !strings.Contains(stdout, "DC0_C0_H0") {
					t.Errorf("get %s: stdout = %q; want the host that was returned", kind, stdout)
				}
			}
		})
	}
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
