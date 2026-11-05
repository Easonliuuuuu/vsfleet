package vsphere

import (
	"github.com/vmware/govmomi/vim25/types"
)

// hostNICPolicy maps a standard switch's or port group's teaming, offload
// and shaping policy. vSphere reports bandwidths in bit/s and the burst size
// in bytes; they are kept in Kbit/s and KB, and as 0 when shaping has never
// been configured.
func hostNICPolicy(policy *types.HostNetworkPolicy) HostNICPolicy {
	if policy == nil {
		return HostNICPolicy{}
	}
	var out HostNICPolicy
	if teaming := policy.NicTeaming; teaming != nil {
		out.TeamingPolicy = teaming.Policy
		out.ReversePolicy = boolPtrValue(teaming.ReversePolicy)
		out.NotifySwitches = boolPtrValue(teaming.NotifySwitches)
		if teaming.RollingOrder != nil {
			out.Failback = boolPtr(!*teaming.RollingOrder)
		}
	}
	if shaping := policy.ShapingPolicy; shaping != nil {
		out.ShapingAverageKbps = int64Ptr(shaping.AverageBandwidth / 1000)
		out.ShapingPeakKbps = int64Ptr(shaping.PeakBandwidth / 1000)
		out.ShapingBurstKB = int64Ptr(shaping.BurstSize / 1024)
	}
	if offload := policy.OffloadPolicy; offload != nil {
		out.ChecksumOffload = boolPtrValue(offload.CsumOffload)
		out.TCPSegmentation = boolPtrValue(offload.TcpSegmentation)
		out.ZeroCopyTransmit = boolPtrValue(offload.ZeroCopyXmit)
	}
	return out
}

// dvsTrafficShaping maps a distributed port setting's ingress and egress
// shaping, converted as hostNICPolicy does.
func dvsTrafficShaping(setting *types.VMwareDVSPortSetting) DVSTrafficShaping {
	var out DVSTrafficShaping
	out.IngressShaping, out.IngressAverageKbps, out.IngressPeakKbps, out.IngressBurstKB = dvsShaping(setting.InShapingPolicy)
	out.EgressShaping, out.EgressAverageKbps, out.EgressPeakKbps, out.EgressBurstKB = dvsShaping(setting.OutShapingPolicy)
	return out
}

func dvsShaping(policy *types.DVSTrafficShapingPolicy) (enabled *bool, average, peak, burst *int64) {
	if policy == nil {
		return nil, nil, nil, nil
	}
	return boolPolicy(policy.Enabled), int64Ptr(longPolicy(policy.AverageBandwidth) / 1000),
		int64Ptr(longPolicy(policy.PeakBandwidth) / 1000), int64Ptr(longPolicy(policy.BurstSize) / 1024)
}

func longPolicy(policy *types.LongPolicy) int64 {
	if policy == nil {
		return 0
	}
	return policy.Value
}

func intPolicy(policy *types.IntPolicy) int32 {
	if policy == nil {
		return 0
	}
	return policy.Value
}

func boolPolicyValue(policy *types.BoolPolicy) bool {
	return policy != nil && policy.Value != nil && *policy.Value
}

// dvsFailureCriteria maps how a distributed port group's teaming detects a
// failed uplink.
func dvsFailureCriteria(teaming *types.VmwareUplinkPortTeamingPolicy) *DVSFailureCriteria {
	if teaming == nil {
		return nil
	}
	c := teaming.FailureCriteria
	if c == nil {
		return &DVSFailureCriteria{}
	}
	return &DVSFailureCriteria{
		CheckSpeed:        stringPolicy(c.CheckSpeed),
		SpeedMbps:         intPolicy(c.Speed),
		CheckDuplex:       boolPolicyValue(c.CheckDuplex),
		FullDuplex:        boolPolicyValue(c.FullDuplex),
		CheckErrorPercent: boolPolicyValue(c.CheckErrorPercent),
		ErrorPercent:      intPolicy(c.Percentage),
		CheckBeacon:       boolPolicyValue(c.CheckBeacon),
	}
}

// dvPortGroupOverrides maps the per-port overrides a distributed port group
// allows. Every distributed port group has a policy; one of another vendor's
// switch has no VLAN, teaming or security overrides, which read as false.
func dvPortGroupOverrides(policy types.BaseDVPortgroupPolicy) *DVPortGroupOverrides {
	if policy == nil {
		return nil
	}
	base := policy.GetDVPortgroupPolicy()
	out := &DVPortGroupOverrides{
		Blocked:                     base.BlockOverrideAllowed,
		Shaping:                     base.ShapingOverrideAllowed,
		VendorConfig:                base.VendorConfigOverrideAllowed,
		LivePortMoving:              base.LivePortMovingAllowed,
		PortConfigResetAtDisconnect: base.PortConfigResetAtDisconnect,
	}
	if vmware, ok := policy.(*types.VMwareDVSPortgroupPolicy); ok {
		out.SecurityPolicy = vmware.SecurityPolicyOverrideAllowed
		out.UplinkTeaming = vmware.UplinkTeamingOverrideAllowed
		out.VLAN = vmware.VlanOverrideAllowed
	}
	return out
}

// dvsLACPGroups maps a distributed switch's link aggregation groups.
func dvsLACPGroups(groups []types.VMwareDvsLacpGroupConfig) []DVSLACPGroup {
	if len(groups) == 0 {
		return nil
	}
	out := make([]DVSLACPGroup, len(groups))
	for i, g := range groups {
		out[i] = DVSLACPGroup{Name: g.Name, Mode: g.Mode, LoadBalanceAlgorithm: g.LoadbalanceAlgorithm}
	}
	return out
}
