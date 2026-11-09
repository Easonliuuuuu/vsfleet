package vsphere_test

import (
	"context"
	"testing"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
	"github.com/vmware/govmomi/vsan"
	vsanmethods "github.com/vmware/govmomi/vsan/methods"
	vsantypes "github.com/vmware/govmomi/vsan/types"
)

// fakeVSAN answers the two vSAN calls the reserve collector makes. vcsim's
// own vSAN simulator implements neither the capacity reservation settings
// nor VsanQuerySpaceUsage, so the replies here follow the vSAN API's shape;
// they have not been compared with a real vSAN cluster.
var fakeVSAN struct {
	reservation *vsantypes.VsanCapacityReservationInfo
	usage       vsantypes.VsanSpaceUsage
}

type fakeVSANConfigSystem struct{ types.ManagedObjectReference }

func (s *fakeVSANConfigSystem) VsanClusterGetConfig(_ *simulator.Context, _ *vsantypes.VsanClusterGetConfig) soap.HasFault {
	return &vsanmethods.VsanClusterGetConfigBody{Res: &vsantypes.VsanClusterGetConfigResponse{Returnval: vsantypes.VsanConfigInfoEx{
		ExtendedConfig: &vsantypes.VsanExtendedConfig{CapacityReservationInfo: fakeVSAN.reservation},
	}}}
}

type fakeVSANSpaceReportSystem struct{ types.ManagedObjectReference }

func (s *fakeVSANSpaceReportSystem) VsanQuerySpaceUsage(_ *simulator.Context, _ *vsantypes.VsanQuerySpaceUsage) soap.HasFault {
	return &vsanmethods.VsanQuerySpaceUsageBody{Res: &vsantypes.VsanQuerySpaceUsageResponse{Returnval: fakeVSAN.usage}}
}

func init() {
	simulator.RegisterEndpoint(func(s *simulator.Service, r *simulator.Registry) {
		if !r.IsVPX() {
			return
		}
		reg := simulator.NewRegistry()
		reg.Namespace, reg.Path = vsan.Namespace, vsan.Path
		reg.Put(&fakeVSANConfigSystem{vsan.VsanVcClusterConfigSystemInstance})
		reg.Put(&fakeVSANSpaceReportSystem{types.ManagedObjectReference{Type: "VsanSpaceReportSystem", Value: "vsan-cluster-space-report-system"}})
		s.RegisterSDK(reg)
	})
}

func TestListDatastoresReadsVSANReserves(t *testing.T) {
	fakeVSAN.reservation = &vsantypes.VsanCapacityReservationInfo{VsanOpSpaceThreshold: "Enforced", HostRebuildThreshold: "Reported"}
	fakeVSAN.usage = vsantypes.VsanSpaceUsage{SpaceDetail: &vsantypes.VsanSpaceUsageDetailResult{SpaceUsageByObjectType: []vsantypes.VsanObjectSpaceSummary{
		{ObjType: "vdisk", UsedB: 1 << 40},
		{ObjType: "minSpaceRequiredForVsanOp", UsedB: 30 << 30},
		{ObjType: "hostRebuildCapacity", ReservedCapacityB: 70 << 30},
	}}}
	t.Cleanup(func() { fakeVSAN.reservation, fakeVSAN.usage = nil, vsantypes.VsanSpaceUsage{} })

	model := simulator.VPX()
	model.Datastore = 2
	if err := model.Create(); err != nil {
		t.Fatalf("create simulator model: %v", err)
	}
	t.Cleanup(model.Remove)
	model.Service.RegisterEndpoints = true

	ctx := context.Background()
	gc, endpoint := dialSimulator(t, model)
	finder := find.NewFinder(gc.Client, true)
	if _, err := finder.DefaultDatacenter(ctx); err != nil {
		t.Fatalf("datacenter: %v", err)
	}
	ds, err := finder.Datastore(ctx, "/DC0/datastore/LocalDS_0")
	if err != nil {
		t.Fatalf("datastore: %v", err)
	}
	model.Map().Get(ds.Reference()).(*simulator.Datastore).Summary.Type = "vsan"

	datastores, err := clientFor(gc, endpoint, "").ListDatastores(ctx)
	if err != nil {
		t.Fatalf("ListDatastores: %v", err)
	}
	seen := 0
	for _, d := range datastores {
		switch d.Name {
		case "LocalDS_0":
			seen++
			r := d.VSANReserve
			if r == nil {
				t.Fatal("vSAN datastore reserve not read")
			}
			if r.OperationsState != "Enforced" || r.OperationsBytes != 30<<30 || r.HostRebuildState != "Reported" || r.HostRebuildBytes != 70<<30 {
				t.Errorf("reserve = %+v, want operations Enforced 30 GiB and host rebuild Reported 70 GiB", *r)
			}
			if got := r.EnforcedBytes(); got != 30<<30 {
				t.Errorf("EnforcedBytes = %d, want only the enforced operations reserve", got)
			}
		case "LocalDS_1":
			seen++
			if d.VSANReserve != nil {
				t.Errorf("VMFS datastore reserve = %+v, want not read", *d.VSANReserve)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("listed %d of LocalDS_0 and LocalDS_1", seen)
	}
}
