package vsphere

import (
	"reflect"
	"testing"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// The raw policies are what the nested lab's vCenter 8.0.3 returns for
// vSwitch0 on 192.168.150.12 and dvpg-mgmt on dvSwitch-Lab (2026-11-05).
func TestHostNICPolicy(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name   string
		policy *types.HostNetworkPolicy
		want   HostNICPolicy
	}{
		{"lab vSwitch0, shaping never configured", &types.HostNetworkPolicy{
			NicTeaming:    &types.HostNicTeamingPolicy{Policy: "loadbalance_srcid", ReversePolicy: &yes, NotifySwitches: &yes, RollingOrder: &no},
			OffloadPolicy: &types.HostNetOffloadCapabilities{CsumOffload: &yes, TcpSegmentation: &yes, ZeroCopyXmit: &yes},
			ShapingPolicy: &types.HostNetworkTrafficShapingPolicy{Enabled: &no},
		}, HostNICPolicy{TeamingPolicy: "loadbalance_srcid", ReversePolicy: &yes, NotifySwitches: &yes, Failback: &yes,
			ShapingAverageKbps: int64Ptr(0), ShapingPeakKbps: int64Ptr(0), ShapingBurstKB: int64Ptr(0),
			ChecksumOffload: &yes, TCPSegmentation: &yes, ZeroCopyTransmit: &yes}},
		{"shaping configured, rolling order on", &types.HostNetworkPolicy{
			NicTeaming:    &types.HostNicTeamingPolicy{RollingOrder: &yes},
			ShapingPolicy: &types.HostNetworkTrafficShapingPolicy{Enabled: &yes, AverageBandwidth: 100000000, PeakBandwidth: 200000000, BurstSize: 104857600},
		}, HostNICPolicy{Failback: &no, ShapingAverageKbps: int64Ptr(100000), ShapingPeakKbps: int64Ptr(200000), ShapingBurstKB: int64Ptr(102400)}},
		{"no policy reported", nil, HostNICPolicy{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hostNICPolicy(tc.policy); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("hostNICPolicy() = %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestMapDVPortGroupPolicy(t *testing.T) {
	yes, no := true, false
	shaping := &types.DVSTrafficShapingPolicy{Enabled: &types.BoolPolicy{Value: &no}, AverageBandwidth: &types.LongPolicy{Value: 100000000},
		PeakBandwidth: &types.LongPolicy{Value: 100000000}, BurstSize: &types.LongPolicy{Value: 104857600}}
	group := &mo.DistributedVirtualPortgroup{Config: types.DVPortgroupConfigInfo{
		Name: "dvpg-mgmt",
		Policy: &types.VMwareDVSPortgroupPolicy{DVPortgroupPolicy: types.DVPortgroupPolicy{BlockOverrideAllowed: true, PortConfigResetAtDisconnect: true},
			VlanOverrideAllowed: true},
		DefaultPortConfig: &types.VMwareDVSPortSetting{
			DVPortSetting: types.DVPortSetting{InShapingPolicy: shaping, OutShapingPolicy: shaping},
			UplinkTeamingPolicy: &types.VmwareUplinkPortTeamingPolicy{
				Policy:        &types.StringPolicy{Value: "loadbalance_srcmac"},
				ReversePolicy: &types.BoolPolicy{Value: &yes},
				FailureCriteria: &types.DVSFailureCriteria{CheckSpeed: &types.StringPolicy{Value: "minimum"}, Speed: &types.IntPolicy{Value: 10},
					CheckDuplex: &types.BoolPolicy{Value: &no}, FullDuplex: &types.BoolPolicy{Value: &no}, CheckErrorPercent: &types.BoolPolicy{Value: &no},
					Percentage: &types.IntPolicy{}, CheckBeacon: &types.BoolPolicy{Value: &no}},
			},
		},
	}}
	got := mapDVPortGroup(group, "dvSwitch-Lab")
	wantShaping := DVSTrafficShaping{IngressShaping: &no, IngressAverageKbps: int64Ptr(100000), IngressPeakKbps: int64Ptr(100000), IngressBurstKB: int64Ptr(102400),
		EgressShaping: &no, EgressAverageKbps: int64Ptr(100000), EgressPeakKbps: int64Ptr(100000), EgressBurstKB: int64Ptr(102400)}
	if !reflect.DeepEqual(got.DVSTrafficShaping, wantShaping) {
		t.Errorf("shaping = %+v\nwant %+v", got.DVSTrafficShaping, wantShaping)
	}
	if got.ReversePolicy == nil || !*got.ReversePolicy {
		t.Errorf("ReversePolicy = %v, want true", got.ReversePolicy)
	}
	if want := (&DVSFailureCriteria{CheckSpeed: "minimum", SpeedMbps: 10}); !reflect.DeepEqual(got.FailureCriteria, want) {
		t.Errorf("FailureCriteria = %+v, want %+v", got.FailureCriteria, want)
	}
	if want := (&DVPortGroupOverrides{Blocked: true, PortConfigResetAtDisconnect: true, VLAN: true}); !reflect.DeepEqual(got.Overrides, want) {
		t.Errorf("Overrides = %+v, want %+v", got.Overrides, want)
	}
}
