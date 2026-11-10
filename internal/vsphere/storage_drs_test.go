package vsphere_test

import (
	"context"
	"testing"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/types"
)

func TestListDatastoresReadsStorageDRS(t *testing.T) {
	model := simulator.VPX()
	model.Datastore = 2
	if err := model.Create(); err != nil {
		t.Fatalf("create simulator model: %v", err)
	}
	t.Cleanup(model.Remove)

	ctx := context.Background()
	gc, endpoint := dialSimulator(t, model)
	finder := find.NewFinder(gc.Client, true)
	dc, err := finder.DefaultDatacenter(ctx)
	if err != nil {
		t.Fatalf("datacenter: %v", err)
	}
	finder.SetDatacenter(dc)
	folders, err := dc.Folders(ctx)
	if err != nil {
		t.Fatalf("folders: %v", err)
	}
	pod, err := folders.DatastoreFolder.CreateStoragePod(ctx, "pod-a")
	if err != nil {
		t.Fatalf("create pod: %v", err)
	}
	ds, err := finder.Datastore(ctx, "LocalDS_0")
	if err != nil {
		t.Fatalf("datastore: %v", err)
	}
	task, err := pod.MoveInto(ctx, []types.ManagedObjectReference{ds.Reference()})
	if err == nil {
		err = task.Wait(ctx)
	}
	if err != nil {
		t.Fatalf("move datastore into pod: %v", err)
	}
	// vcsim's ConfigureStorageDrsForPod ignores the space configuration, so
	// set it as vCenter would report it.
	sim := model.Map().Get(pod.Reference()).(*simulator.StoragePod)
	sim.PodStorageDrsEntry.StorageDrsConfig.PodConfig = types.StorageDrsPodConfigInfo{
		Enabled:                true,
		SpaceLoadBalanceConfig: &types.StorageDrsSpaceLoadBalanceConfig{SpaceThresholdMode: "utilization", SpaceUtilizationThreshold: 85},
	}

	c := clientFor(t, endpoint, "")
	datastores, err := c.ListDatastores(ctx)
	if err != nil {
		t.Fatalf("ListDatastores: %v", err)
	}
	for _, d := range datastores {
		if d.Name != "LocalDS_0" {
			continue
		}
		cl := d.Details.StorageCluster
		if cl == nil || cl.ID != pod.Reference().Value || cl.Name != "pod-a" || cl.StorageDRS == nil {
			t.Fatalf("storage cluster = %+v, want pod-a with Storage DRS read", cl)
		}
		if s := *cl.StorageDRS; !s.Enabled || s.SpaceThresholdMode != "utilization" || s.SpaceUtilizationPct != 85 {
			t.Errorf("Storage DRS = %+v, want enabled at 85%% used", s)
		}
		return
	}
	t.Fatal("LocalDS_0 not listed")
}
