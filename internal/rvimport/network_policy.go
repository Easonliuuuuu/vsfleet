package rvimport

import (
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// hostNICPolicyFromRow reads the vSwitch and vPort teaming, shaping and
// offload columns inventory schema 36 added. Rolling Order is vSphere's raw
// value, the opposite of failback.
func hostNICPolicyFromRow(t sheetTable, row []string) vsphere.HostNICPolicy {
	return vsphere.HostNICPolicy{
		TeamingPolicy:      t.cell(row, "Policy"),
		ReversePolicy:      parseOptBool(t.cell(row, "Reverse Policy")),
		NotifySwitches:     parseOptBool(t.cell(row, "Notify Switch")),
		Failback:           invertedOptBool(t.cell(row, "Rolling Order")),
		ShapingAverageKbps: parseOptInt64(t.cell(row, "Width")),
		ShapingPeakKbps:    parseOptInt64(t.cell(row, "Peak")),
		ShapingBurstKB:     parseOptInt64(t.cell(row, "Burst")),
		ChecksumOffload:    parseOptBool(t.cell(row, "Offload")),
		TCPSegmentation:    parseOptBool(t.cell(row, "TSO")),
		ZeroCopyTransmit:   parseOptBool(t.cell(row, "Zero Copy Xmit")),
	}
}

func invertedOptBool(s string) *bool {
	v := parseOptBool(s)
	if v != nil {
		*v = !*v
	}
	return v
}

// dvsTrafficShapingFromRow reads dvSwitch's or dvPort's In and Out shaping
// columns. A workbook from before inventory schema 36 names the two shaping
// flags Ingress shaping and Egress shaping, which the header aliases accept.
func dvsTrafficShapingFromRow(t sheetTable, row []string) vsphere.DVSTrafficShaping {
	return vsphere.DVSTrafficShaping{
		IngressShaping:     parseOptBool(t.cell(row, "Ingress shaping")),
		IngressAverageKbps: parseOptInt64(t.cell(row, "In Avg")),
		IngressPeakKbps:    parseOptInt64(t.cell(row, "In Peak")),
		IngressBurstKB:     parseOptInt64(t.cell(row, "In Burst")),
		EgressShaping:      parseOptBool(t.cell(row, "Egress shaping")),
		EgressAverageKbps:  parseOptInt64(t.cell(row, "Out Avg")),
		EgressPeakKbps:     parseOptInt64(t.cell(row, "Out Peak")),
		EgressBurstKB:      parseOptInt64(t.cell(row, "Out Burst")),
	}
}

// applyDVSwitchPolicyCells reads the dvSwitch columns inventory schema 36
// added. RVTools' Contact is the contact detail and Admin Name the contact's
// name; a workbook without Admin Name is vsfleet's earlier layout, where
// Contact was the name and Contact detail the detail.
func applyDVSwitchPolicyCells(t sheetTable, row []string, sw *vsphere.DVSwitch, timezone *time.Location) {
	if t.hasSourceHeader("Admin Name") {
		sw.Contact, sw.ContactDetail = t.cell(row, "Admin Name"), t.cell(row, "Contact")
	} else {
		sw.Contact, sw.ContactDetail = t.cell(row, "Contact"), t.cell(row, "Contact detail")
	}
	sw.ProductName = t.cell(row, "Name")
	sw.Created = optionalTime(t.cell(row, "Created"), timezone)
	if v := parseOptInt32(t.cell(row, "# VMs")); v != nil {
		vms := int(*v)
		sw.VMCount = &vms
	}
	sw.LACPGroups = lacpGroupsFromRow(t, row)
	sw.DVSTrafficShaping = dvsTrafficShapingFromRow(t, row)
}

// lacpGroupsFromRow pairs the comma-separated LACP Name, LACP Mode and LACP
// Load Balance Alg. cells, one link aggregation group per name.
func lacpGroupsFromRow(t sheetTable, row []string) []vsphere.DVSLACPGroup {
	names := splitList(t.cell(row, "LACP Name"))
	modes, algorithms := splitList(t.cell(row, "LACP Mode")), splitList(t.cell(row, "LACP Load Balance Alg."))
	if len(names) == 0 {
		return nil
	}
	out := make([]vsphere.DVSLACPGroup, len(names))
	for i, name := range names {
		out[i].Name = name
		if i < len(modes) {
			out[i].Mode = modes[i]
		}
		if i < len(algorithms) {
			out[i].LoadBalanceAlgorithm = algorithms[i]
		}
	}
	return out
}

// applyDVPortPolicyCells reads the dvPort columns inventory schema 36 added.
// The failure criteria and overrides are each left nil when the workbook
// has none of their columns.
func applyDVPortPolicyCells(t sheetTable, row []string, p *vsphere.DVPortGroup) {
	p.ReversePolicy = parseOptBool(t.cell(row, "Reverse Policy"))
	p.DVSTrafficShaping = dvsTrafficShapingFromRow(t, row)
	if t.has("Check Speed") {
		p.FailureCriteria = &vsphere.DVSFailureCriteria{
			CheckSpeed:        t.cell(row, "Check Speed"),
			SpeedMbps:         int32(parseInt(t.cell(row, "Speed"))),
			CheckDuplex:       parseBool(t.cell(row, "Check Duplex")),
			FullDuplex:        parseBool(t.cell(row, "Full Duplex")),
			CheckErrorPercent: parseBool(t.cell(row, "Check Error %")),
			ErrorPercent:      int32(parseInt(t.cell(row, "Percentage"))),
			CheckBeacon:       parseBool(t.cell(row, "Check Beacon")),
		}
	}
	if t.has("Block Override") {
		p.Overrides = &vsphere.DVPortGroupOverrides{
			Blocked:                     parseBool(t.cell(row, "Block Override")),
			Shaping:                     parseBool(t.cell(row, "Shaping Override")),
			VendorConfig:                parseBool(t.cell(row, "Vendor Config Override")),
			SecurityPolicy:              parseBool(t.cell(row, "Sec. Policy Override")),
			UplinkTeaming:               parseBool(t.cell(row, "Teaming Override")),
			VLAN:                        parseBool(t.cell(row, "Vlan Override")),
			LivePortMoving:              parseBool(t.cell(row, "Live Port Moving")),
			PortConfigResetAtDisconnect: parseBool(t.cell(row, "Config Reset")),
		}
	}
}
