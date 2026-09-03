package vsphere_test

import (
	"context"
	"testing"

	"github.com/vmware/govmomi/simulator"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestGroupForMapsEveryKind(t *testing.T) {
	cases := []struct {
		kind vsphere.Kind
		want vsphere.FetchGroup
	}{
		{vsphere.KindVM, vsphere.GroupVMs},
		{vsphere.KindTemplate, vsphere.GroupVMs},
		{vsphere.KindHost, vsphere.GroupHosts},
		{vsphere.KindCluster, vsphere.GroupClusters},
		{vsphere.KindVApp, vsphere.GroupVApps},
		{vsphere.KindDatastore, vsphere.GroupDatastores},
		{vsphere.KindNetwork, vsphere.GroupNetworks},
	}
	for _, tc := range cases {
		if got := vsphere.GroupFor(tc.kind); got != tc.want {
			t.Errorf("GroupFor(%s) = %s, want %s", tc.kind, got, tc.want)
		}
	}
	// Every kind maps to some group in AllGroups, and every group is
	// reachable from at least one kind — the fetch-group and tab-kind
	// vocabularies must stay in lockstep, or a tab would prioritize a group
	// that does not exist.
	seen := map[vsphere.FetchGroup]bool{}
	for _, k := range vsphere.AllKinds {
		g := vsphere.GroupFor(k)
		if g == "" {
			t.Errorf("GroupFor(%s) returned no group", k)
		}
		seen[g] = true
	}
	for _, g := range vsphere.AllGroups {
		if !seen[g] {
			t.Errorf("group %s is in AllGroups but no kind maps to it", g)
		}
	}
}

// TestFetchGroupPopulatesOnlyItsOwnKinds checks the isolation FetchGroup
// promises: retrieving one group never touches the Inventory fields another
// group owns, which is what lets a caller merge several groups' results
// (via Inventory.ApplyGroup) without one silently clobbering another.
func TestFetchGroupPopulatesOnlyItsOwnKinds(t *testing.T) {
	c, _ := newSimulator(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 1
		m.App = 1
		m.Machine = 1
		m.Datastore = 1
	})
	ctx := context.Background()

	idx, err := c.NewIndex(ctx)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}

	empty := func(inv *vsphere.Inventory) bool {
		return len(inv.VMs) == 0 && len(inv.Templates) == 0 && len(inv.Hosts) == 0 &&
			len(inv.Clusters) == 0 && len(inv.VApps) == 0 && len(inv.Datastores) == 0 && len(inv.Networks) == 0
	}

	for _, tc := range []struct {
		group    vsphere.FetchGroup
		nonEmpty func(*vsphere.Inventory) bool
	}{
		{vsphere.GroupVMs, func(i *vsphere.Inventory) bool { return len(i.VMs) > 0 }},
		{vsphere.GroupHosts, func(i *vsphere.Inventory) bool { return len(i.Hosts) > 0 }},
		{vsphere.GroupClusters, func(i *vsphere.Inventory) bool { return len(i.Clusters) > 0 }},
		{vsphere.GroupVApps, func(i *vsphere.Inventory) bool { return len(i.VApps) > 0 }},
		{vsphere.GroupDatastores, func(i *vsphere.Inventory) bool { return len(i.Datastores) > 0 }},
		{vsphere.GroupNetworks, func(i *vsphere.Inventory) bool { return len(i.Networks) > 0 }},
	} {
		inv := c.FetchGroup(ctx, idx, tc.group)
		if len(inv.Errors) != 0 {
			t.Errorf("%s: unexpected errors %v", tc.group, inv.Errors)
		}
		if !tc.nonEmpty(inv) {
			t.Errorf("%s: fetched nothing for its own kind(s): %+v", tc.group, inv)
		}
		blanked := *inv
		switch tc.group {
		case vsphere.GroupVMs:
			blanked.VMs, blanked.Templates = nil, nil
		case vsphere.GroupHosts:
			blanked.Hosts = nil
		case vsphere.GroupClusters:
			blanked.Clusters = nil
		case vsphere.GroupVApps:
			blanked.VApps = nil
		case vsphere.GroupDatastores:
			blanked.Datastores = nil
		case vsphere.GroupNetworks:
			blanked.Networks = nil
		}
		if !empty(&blanked) {
			t.Errorf("%s: populated a field it does not own: %+v", tc.group, inv)
		}
	}
}

func TestApplyGroupReplacesOnSuccessAndPreservesOnFailure(t *testing.T) {
	inv := &vsphere.Inventory{
		Context: "lab",
		Hosts:   []vsphere.Host{{Name: "stale-host"}},
		Errors:  []vsphere.InventoryError{{Kind: vsphere.KindDatastore, Message: "old permission error"}},
	}

	// A successful group replaces its own fields and clears any Errors
	// entry it previously carried.
	inv.ApplyGroup(vsphere.GroupHosts, &vsphere.Inventory{Hosts: []vsphere.Host{{Name: "fresh-host"}}})
	if len(inv.Hosts) != 1 || inv.Hosts[0].Name != "fresh-host" {
		t.Fatalf("hosts after a successful refresh = %v, want only fresh-host", inv.Hosts)
	}

	// A failed group leaves the existing (stale) data for its kind
	// untouched, and records the new error.
	inv.Hosts = []vsphere.Host{{Name: "fresh-host"}} // pretend this was the last good read
	inv.ApplyGroup(vsphere.GroupHosts, &vsphere.Inventory{
		Errors: []vsphere.InventoryError{{Kind: vsphere.KindHost, Message: "connection reset"}},
	})
	if len(inv.Hosts) != 1 || inv.Hosts[0].Name != "fresh-host" {
		t.Errorf("a failed refresh must keep the stale hosts, got %v", inv.Hosts)
	}
	msg, failed := inv.ErrorFor(vsphere.KindHost)
	if !failed || msg != "connection reset" {
		t.Errorf("ErrorFor(host) = (%q, %v), want (\"connection reset\", true)", msg, failed)
	}

	// The unrelated datastore error from before is untouched by either call.
	if msg, failed := inv.ErrorFor(vsphere.KindDatastore); !failed || msg != "old permission error" {
		t.Errorf("unrelated datastore error was disturbed: (%q, %v)", msg, failed)
	}

	// Now datastores succeed: its old error clears, without touching the
	// still-broken hosts.
	inv.ApplyGroup(vsphere.GroupDatastores, &vsphere.Inventory{Datastores: []vsphere.Datastore{{Name: "ds0"}}})
	if _, failed := inv.ErrorFor(vsphere.KindDatastore); failed {
		t.Error("datastore error should have cleared once the group succeeded")
	}
	if _, failed := inv.ErrorFor(vsphere.KindHost); !failed {
		t.Error("host error should still be recorded — only datastores were retried")
	}
}

// TestApplyGroupOnBlankInventoryBuildsTheWholeBundle checks the first-load
// case ListInventory relies on: applying every group in turn to a blank
// Inventory produces exactly what the old, hand-merged ListInventory did.
func TestApplyGroupOnBlankInventoryBuildsTheWholeBundle(t *testing.T) {
	inv := &vsphere.Inventory{Context: "lab"}
	inv.ApplyGroup(vsphere.GroupVMs, &vsphere.Inventory{
		VMs:       []vsphere.VM{{Name: "vm0"}},
		Templates: []vsphere.VM{{Name: "tpl0", IsTemplate: true}},
	})
	inv.ApplyGroup(vsphere.GroupHosts, &vsphere.Inventory{Hosts: []vsphere.Host{{Name: "h0"}}})
	inv.ApplyGroup(vsphere.GroupClusters, &vsphere.Inventory{
		Errors: []vsphere.InventoryError{{Kind: vsphere.KindCluster, Message: "denied"}},
	})

	if len(inv.VMs) != 1 || len(inv.Templates) != 1 || len(inv.Hosts) != 1 {
		t.Fatalf("bundle is missing successful groups: %+v", inv)
	}
	if len(inv.Clusters) != 0 {
		t.Errorf("a denied group should contribute nothing, got %v", inv.Clusters)
	}
	if msg, failed := inv.ErrorFor(vsphere.KindCluster); !failed || msg != "denied" {
		t.Errorf("ErrorFor(cluster) = (%q, %v), want (\"denied\", true)", msg, failed)
	}
}
