package vsphere_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/vmware/govmomi/simulator"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// manyVMs builds a simulator estate with enough virtual machines that a
// small page size genuinely splits the result, which is the only way to
// exercise the continuation the paged read exists for.
func manyVMs(m *simulator.Model) {
	m.Datacenter = 1
	m.Cluster = 1
	m.ClusterHost = 2
	m.Machine = 12
}

// TestPagedFetchMatchesUnpagedFetch is the property the paged read has to
// hold above all others: reading an estate a page at a time must produce
// exactly the estate, not a prefix of it and not a duplicate of anything.
func TestPagedFetchMatchesUnpagedFetch(t *testing.T) {
	c, _ := newSimulator(t, manyVMs)
	ctx := context.Background()
	idx, err := c.NewIndex(ctx)
	if err != nil {
		t.Fatalf("new index: %v", err)
	}

	whole := c.FetchGroup(ctx, idx, vsphere.GroupVMs)
	if len(whole.VMs) < 4 {
		t.Fatalf("test estate has %d VMs, too few to page", len(whole.VMs))
	}

	var pages int
	var streamed int
	paged := c.FetchGroupWith(ctx, idx, vsphere.GroupVMs, vsphere.FetchOptions{
		Detail:   vsphere.DetailFull,
		PageSize: 3,
		OnPartial: func(part *vsphere.Inventory) {
			pages++
			streamed += len(part.VMs) + len(part.Templates)
		},
	})
	if pages < 2 {
		t.Errorf("page size 3 over %d VMs produced %d page(s); the continuation never ran", len(whole.VMs), pages)
	}
	if want := len(whole.VMs) + len(whole.Templates); streamed != want {
		t.Errorf("pages carried %d objects in total, want %d", streamed, want)
	}
	if len(paged.VMs) != len(whole.VMs) || len(paged.Templates) != len(whole.Templates) {
		t.Fatalf("paged fetch = %d VMs / %d templates, unpaged = %d / %d",
			len(paged.VMs), len(paged.Templates), len(whole.VMs), len(whole.Templates))
	}
	for i := range whole.VMs {
		if !reflect.DeepEqual(paged.VMs[i], whole.VMs[i]) {
			t.Errorf("VM %d differs between the paged and unpaged reads:\npaged   %+v\nunpaged %+v", i, paged.VMs[i], whole.VMs[i])
		}
	}
}

// TestPagedFetchReturnsVMsInOrder checks that a caller showing pages as they
// land is shown an ordered list, since the server hands them over in its own
// traversal order rather than by name.
func TestPagedFetchReturnsVMsInOrder(t *testing.T) {
	c, _ := newSimulator(t, manyVMs)
	ctx := context.Background()
	idx, err := c.NewIndex(ctx)
	if err != nil {
		t.Fatalf("new index: %v", err)
	}
	inv := c.FetchGroupWith(ctx, idx, vsphere.GroupVMs, vsphere.FetchOptions{PageSize: 3})
	for i := 1; i < len(inv.VMs); i++ {
		if inv.VMs[i-1].Name > inv.VMs[i].Name {
			t.Fatalf("VMs out of order at %d: %q before %q", i, inv.VMs[i-1].Name, inv.VMs[i].Name)
		}
	}
}

// TestDetailSummarySkipsTheExpensiveProperties is the point of the whole
// split: a listing must not pay for device inventory, guest NIC bindings and
// snapshot trees it never shows. Comparing the two levels side by side keeps
// the test honest about what is dropped and what has to survive.
func TestDetailSummarySkipsTheExpensiveProperties(t *testing.T) {
	c, _ := newSimulator(t, manyVMs)
	ctx := context.Background()
	idx, err := c.NewIndex(ctx)
	if err != nil {
		t.Fatalf("new index: %v", err)
	}

	full := c.FetchGroupWith(ctx, idx, vsphere.GroupVMs, vsphere.FetchOptions{Detail: vsphere.DetailFull, PageSize: -1})
	summary := c.FetchGroupWith(ctx, idx, vsphere.GroupVMs, vsphere.FetchOptions{Detail: vsphere.DetailSummary})

	if len(full.VMs) != len(summary.VMs) {
		t.Fatalf("summary saw %d VMs, full saw %d", len(summary.VMs), len(full.VMs))
	}
	devices := 0
	for _, vm := range full.VMs {
		devices += len(vm.Disks) + len(vm.NICs)
	}
	if devices == 0 {
		t.Fatal("the full detail level retrieved no devices at all; the test proves nothing")
	}
	for i, vm := range summary.VMs {
		if len(vm.Disks) != 0 || len(vm.NICs) != 0 || len(vm.Snapshots) != 0 {
			t.Errorf("%s: summary detail carried %d disks, %d NICs, %d snapshots; it should carry none",
				vm.Name, len(vm.Disks), len(vm.NICs), len(vm.Snapshots))
		}
		// Everything a table row, a detail pane or a search result reads has
		// to survive the cheaper retrieval, or the interface would be fast
		// and wrong instead of slow and right.
		ref := full.VMs[i]
		if vm.Name != ref.Name || vm.PowerState != ref.PowerState || vm.CPU != ref.CPU ||
			vm.MemoryMB != ref.MemoryMB || vm.Host != ref.Host || vm.Cluster != ref.Cluster ||
			vm.Datacenter != ref.Datacenter || vm.Path != ref.Path || vm.GuestOS != ref.GuestOS ||
			vm.IPAddress != ref.IPAddress || vm.InstanceUUID != ref.InstanceUUID ||
			vm.IsTemplate != ref.IsTemplate {
			t.Errorf("%s: summary detail lost a field a listing shows:\nsummary %+v\nfull    %+v", vm.Name, vm, ref)
		}
	}
}

// TestNewIndexIsReusedWithinItsTTL covers the other half of what made a
// refresh expensive: rebuilding the whole estate's folder tree every time.
func TestNewIndexIsReusedWithinItsTTL(t *testing.T) {
	c, _ := newSimulator(t, manyVMs)
	ctx := context.Background()
	first, err := c.NewIndex(ctx)
	if err != nil {
		t.Fatalf("new index: %v", err)
	}
	second, err := c.NewIndex(ctx)
	if err != nil {
		t.Fatalf("new index again: %v", err)
	}
	if first != second {
		t.Error("a second NewIndex within IndexTTL walked the inventory again instead of reusing the first")
	}
}
