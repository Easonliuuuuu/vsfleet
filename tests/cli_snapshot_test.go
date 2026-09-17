// This file exercises issue #158's "snapshot list" command end to end: VM
// snapshot evidence that already exists in the normalized model is queryable
// directly, across every VM or narrowed to one with --vm.
package tests

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/simulator"
)

// createSimulatorSnapshot creates one read-only-irrelevant snapshot on a
// simulated VM, the way an operator would with govc, so "snapshot list" has
// real evidence to report.
func createSimulatorSnapshot(t *testing.T, vc *vcenter, vmPath, name string) {
	t.Helper()
	ctx := context.Background()
	client, err := govmomi.NewClient(ctx, mustURL(t, vc.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Login(ctx, url.UserPassword("user", "pass")); err != nil {
		t.Fatal(err)
	}
	finder := find.NewFinder(client.Client, false)
	vm, err := finder.VirtualMachine(ctx, vmPath)
	if err != nil {
		t.Fatal(err)
	}
	task, err := vm.CreateSnapshot(ctx, name, "created for issue-158 tests", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotListing(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 2
		m.Machine = 2
	})
	createSimulatorSnapshot(t, vc, "/DC0/vm/DC0_C0_RP0_VM0", "baseline")

	r := newRunner(t)
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")

	all := r.mustRun(testPassword+"\n", "snapshot", "list")
	if !strings.Contains(all, "DC0_C0_RP0_VM0") || !strings.Contains(all, "baseline") {
		t.Fatalf("snapshot list did not show the simulated snapshot:\n%s", all)
	}

	narrowed := r.mustRun(testPassword+"\n", "snapshot", "list", "--vm", "DC0_C0_RP0_VM0")
	if !strings.Contains(narrowed, "baseline") {
		t.Fatalf("--vm did not return the VM's snapshot:\n%s", narrowed)
	}

	empty := r.mustRun(testPassword+"\n", "snapshot", "list", "--vm", "DC0_C0_RP0_VM1")
	if strings.Contains(empty, "baseline") {
		t.Fatalf("--vm on a snapshot-free VM leaked another VM's snapshot:\n%s", empty)
	}

	// The same evidence also appears inline on "vm show".
	detail := r.mustRun(testPassword+"\n", "vm", "show", "DC0_C0_RP0_VM0")
	if !strings.Contains(detail, "Snapshots:") || !strings.Contains(detail, "baseline") {
		t.Errorf("vm show did not include its snapshot evidence:\n%s", detail)
	}
}
