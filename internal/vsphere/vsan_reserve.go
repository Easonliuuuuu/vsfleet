package vsphere

import (
	"context"
	"fmt"
	"strings"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	"github.com/vmware/govmomi/vsan"
	"github.com/vmware/govmomi/vsan/methods"
	vsantypes "github.com/vmware/govmomi/vsan/types"

	"github.com/easonliuuuuu/vsfleet/internal/humanize"
)

// VSANCapacityReserve is a vSAN cluster's capacity reserves, as of vSAN 7.0
// U1: the operations reserve, kept free for resyncs and other vSAN
// operations, and the host rebuild reserve, kept free to repair objects
// after a host failure. vSAN blocks new provisioning into a reserve whose
// state is "Enforced"; a "Reported" reserve is only shown.
type VSANCapacityReserve struct {
	// OperationsState and HostRebuildState are each reserve's state:
	// "Enforced", "Reported", "Disabled" or "Unsupported". Empty when vCenter
	// reports none, as before vSAN 7.0 U1.
	OperationsState  string `json:"operations_reserve_state,omitempty"`
	OperationsBytes  int64  `json:"operations_reserve_bytes"`
	HostRebuildState string `json:"host_rebuild_reserve_state,omitempty"`
	HostRebuildBytes int64  `json:"host_rebuild_reserve_bytes"`
}

// VSANReserveEnforced is the state of a reserve vSAN will not provision into.
const VSANReserveEnforced = string(vsantypes.VsanCapacityReservationStateEnforced)

// EnforcedBytes is the space the enforced reserves hold back from new
// provisioning.
func (r VSANCapacityReserve) EnforcedBytes() int64 {
	var n int64
	if r.OperationsState == VSANReserveEnforced {
		n += r.OperationsBytes
	}
	if r.HostRebuildState == VSANReserveEnforced {
		n += r.HostRebuildBytes
	}
	return n
}

// Summary describes both reserves, e.g. "operations 30 GiB (Enforced), host
// rebuild 70 GiB (Reported)"; a reserve vCenter reports no state for is
// "unsupported".
func (r VSANCapacityReserve) Summary() string {
	part := func(name, state string, bytes int64) string {
		if state == "" {
			return name + " unsupported"
		}
		return fmt.Sprintf("%s %s (%s)", name, humanize.Bytes(bytes), state)
	}
	return strings.Join([]string{part("operations", r.OperationsState, r.OperationsBytes), part("host rebuild", r.HostRebuildState, r.HostRebuildBytes)}, ", ")
}

// ProvisionableFreeBytes is the free space new disks can still use: the
// free space less a vSAN datastore's enforced capacity reserves, never
// below zero. It is FreeBytes for any other datastore, and for a vSAN
// datastore whose reserves were not read.
func (d Datastore) ProvisionableFreeBytes() int64 {
	if d.VSANReserve == nil {
		return d.FreeBytes
	}
	return max(d.FreeBytes-d.VSANReserve.EnforcedBytes(), 0)
}

// vsanSpaceReportSystem answers VsanQuerySpaceUsage on vCenter's vSAN
// endpoint; govmomi's vsan client has no wrapper for it.
var vsanSpaceReportSystem = types.ManagedObjectReference{Type: "VsanSpaceReportSystem", Value: "vsan-cluster-space-report-system"}

// vsanReserves reads the capacity reserves of the cluster behind each vSAN
// datastore in raw, by datastore. A vSAN datastore's cluster is the cluster
// of the hosts that mount it. A cluster whose reserves cannot be read, such
// as one on a vCenter without the vSAN health service, is left out: its
// datastores then report reserves as not read, not as none.
func (c *Client) vsanReserves(ctx context.Context, idx *index, raw []mo.Datastore) map[types.ManagedObjectReference]*VSANCapacityReserve {
	clusters := map[types.ManagedObjectReference]types.ManagedObjectReference{}
	for i := range raw {
		if raw[i].Summary.Type != string(types.HostFileSystemVolumeFileSystemTypeVsan) {
			continue
		}
		if cluster, ok := idx.vsanCluster(raw[i].Host); ok {
			clusters[raw[i].Self] = cluster
		}
	}
	if len(clusters) == 0 {
		return nil
	}
	client, err := vsan.NewClient(ctx, c.vim.Client)
	if err != nil {
		return nil
	}
	byCluster := map[types.ManagedObjectReference]*VSANCapacityReserve{}
	out := make(map[types.ManagedObjectReference]*VSANCapacityReserve, len(clusters))
	for ds, cluster := range clusters {
		reserve, done := byCluster[cluster]
		if !done {
			reserve, _ = readVSANReserve(ctx, client, cluster)
			byCluster[cluster] = reserve
		}
		if reserve != nil {
			out[ds] = reserve
		}
	}
	return out
}

// vsanCluster is the cluster of the first mounting host that belongs to one.
func (i *index) vsanCluster(mounts []types.DatastoreHostMount) (types.ManagedObjectReference, bool) {
	for _, mount := range mounts {
		if e, ok := i.byRef[mount.Key]; ok && e.parent != nil && e.parent.Type == "ClusterComputeResource" {
			return *e.parent, true
		}
	}
	return types.ManagedObjectReference{}, false
}

// readVSANReserve reads one cluster's reserve states from its vSAN
// configuration and their sizes from its space usage report.
func readVSANReserve(ctx context.Context, client *vsan.Client, cluster types.ManagedObjectReference) (*VSANCapacityReserve, error) {
	config, err := client.VsanClusterGetConfig(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("vSAN configuration of %s: %w", cluster.Value, err)
	}
	usage, err := methods.VsanQuerySpaceUsage(ctx, client, &vsantypes.VsanQuerySpaceUsage{This: vsanSpaceReportSystem, Cluster: cluster})
	if err != nil {
		return nil, fmt.Errorf("vSAN space usage of %s: %w", cluster.Value, err)
	}
	out := &VSANCapacityReserve{}
	if ext := config.ExtendedConfig; ext != nil && ext.CapacityReservationInfo != nil {
		out.OperationsState = ext.CapacityReservationInfo.VsanOpSpaceThreshold
		out.HostRebuildState = ext.CapacityReservationInfo.HostRebuildThreshold
	}
	if detail := usage.Returnval.SpaceDetail; detail != nil {
		for _, s := range detail.SpaceUsageByObjectType {
			switch vsantypes.VsanObjectTypeEnum(s.ObjType) {
			case vsantypes.VsanObjectTypeEnumminSpaceRequiredForVsanOp:
				out.OperationsBytes = reserveBytes(s)
			case vsantypes.VsanObjectTypeEnumhostRebuildCapacity:
				out.HostRebuildBytes = reserveBytes(s)
			}
		}
	}
	return out, nil
}

// reserveBytes is the size of a reserve's space usage entry. vSAN reports a
// reserve as a pseudo object type; which byte field carries its size has
// not been confirmed against a real vSAN cluster, so the first non-zero of
// the used, reserved and physically used figures is taken.
func reserveBytes(s vsantypes.VsanObjectSpaceSummary) int64 {
	for _, n := range []int64{s.UsedB, s.ReservedCapacityB, s.PhysicalUsedB} {
		if n > 0 {
			return n
		}
	}
	return 0
}
