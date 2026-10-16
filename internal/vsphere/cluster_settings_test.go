package vsphere_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The configuration is what the nested lab's vCenter 8.0.3 reports for
// Cluster-DR: HA off, yet vSphere keeps every HA setting, and RVTools reports
// them.
func TestListClustersReadsSettingsWhileHAIsOff(t *testing.T) {
	model := simulator.VPX()
	if err := model.Create(); err != nil {
		t.Fatalf("create simulator model: %v", err)
	}
	t.Cleanup(model.Remove)

	cluster := model.Map().Any("ClusterComputeResource").(*simulator.ClusterComputeResource)
	cluster.ConfigStatus = types.ManagedEntityStatusYellow
	cfg := cluster.ConfigurationEx.(*types.ClusterConfigInfoEx)
	cfg.DasConfig = types.ClusterDasConfigInfo{
		Enabled: types.NewBool(false), VmMonitoring: "vmMonitoringDisabled", HostMonitoring: "enabled", FailoverLevel: 1,
		AdmissionControlEnabled: types.NewBool(true), HBDatastoreCandidatePolicy: "allFeasibleDsWithUserPreference",
		DefaultVmSettings: &types.ClusterDasVmSettings{
			RestartPriority: "medium", IsolationResponse: "none",
			VmToolsMonitoringSettings: &types.ClusterVmToolsMonitoringSettings{
				ClusterSettings: types.NewBool(true), FailureInterval: 30, MinUpTime: 120, MaxFailures: 3, MaxFailureWindow: -1,
			},
		},
	}
	cfg.DrsConfig = types.ClusterDrsConfigInfo{Enabled: types.NewBool(true), DefaultVmBehavior: types.DrsBehaviorFullyAutomated, VmotionRate: 3}
	cfg.DpmConfigInfo = &types.ClusterDpmConfigInfo{Enabled: types.NewBool(false), DefaultDpmBehavior: types.DpmBehaviorAutomated, HostPowerActionRate: 3}
	summary := cluster.Summary.(*types.ClusterComputeResourceSummary)
	summary.NumCpuThreads, summary.NumVmotions = 8, 2

	gc, endpoint := dialSimulator(t, model)
	clusters, err := clientFor(gc, endpoint, "").ListClusters(context.Background())
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	var got *vsphere.Cluster
	for i := range clusters {
		if clusters[i].ID == cluster.Self.Value {
			got = &clusters[i]
		}
	}
	if got == nil {
		t.Fatalf("cluster %s not listed", cluster.Self.Value)
	}
	if got.HAEnabled || got.HA != nil {
		t.Errorf("HA = %v %+v, want off with no failover state", got.HAEnabled, got.HA)
	}
	if got.ConfigStatus != "yellow" {
		t.Errorf("config status = %q, want yellow", got.ConfigStatus)
	}
	if got.CPUThreads == nil || *got.CPUThreads != 8 || got.VMotions == nil || *got.VMotions != 2 {
		t.Errorf("threads, vMotions = %v, %v, want 8, 2", got.CPUThreads, got.VMotions)
	}
	want := &vsphere.ClusterSettings{
		FailoverLevel: 1, AdmissionControl: true, HostMonitoring: "enabled", VMMonitoring: "vmMonitoringDisabled",
		HeartbeatDatastorePolicy: "allFeasibleDsWithUserPreference", IsolationResponse: "none", RestartPriority: "medium",
		VMTools:        &vsphere.ClusterVMToolsMonitoring{ClusterSettings: types.NewBool(true), MaxFailures: 3, MaxFailureWindow: -1, FailureInterval: 30, MinUpTime: 120},
		DRSVMotionRate: 3, DPMBehavior: "automated", DPMHostPowerActionRate: 3,
	}
	if !reflect.DeepEqual(got.Settings, want) {
		t.Errorf("settings = %+v\nwant %+v", got.Settings, want)
	}
}
