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
// The failure criteria and the overrides are each left nil when all their
// cells are blank, as they are for a port group captured before schema 36
// or a workbook without the columns: a blank is not FALSE or 0.
func applyDVPortPolicyCells(t sheetTable, row []string, p *vsphere.DVPortGroup) {
	p.ReversePolicy = parseOptBool(t.cell(row, "Reverse Policy"))
	p.DVSTrafficShaping = dvsTrafficShapingFromRow(t, row)
	failure := cells(t, row, "Check Speed", "Speed", "Check Duplex", "Full Duplex", "Check Error %", "Percentage", "Check Beacon")
	if anyFilled(failure) {
		p.FailureCriteria = &vsphere.DVSFailureCriteria{
			CheckSpeed:        failure[0],
			SpeedMbps:         int32(parseInt(failure[1])),
			CheckDuplex:       parseBool(failure[2]),
			FullDuplex:        parseBool(failure[3]),
			CheckErrorPercent: parseBool(failure[4]),
			ErrorPercent:      int32(parseInt(failure[5])),
			CheckBeacon:       parseBool(failure[6]),
		}
	}
	overrides := cells(t, row, "Block Override", "Shaping Override", "Vendor Config Override", "Sec. Policy Override", "Teaming Override",
		"Vlan Override", "Live Port Moving", "Config Reset")
	if anyFilled(overrides) {
		p.Overrides = &vsphere.DVPortGroupOverrides{
			Blocked:                     parseBool(overrides[0]),
			Shaping:                     parseBool(overrides[1]),
			VendorConfig:                parseBool(overrides[2]),
			SecurityPolicy:              parseBool(overrides[3]),
			UplinkTeaming:               parseBool(overrides[4]),
			VLAN:                        parseBool(overrides[5]),
			LivePortMoving:              parseBool(overrides[6]),
			PortConfigResetAtDisconnect: parseBool(overrides[7]),
		}
	}
}

func cells(t sheetTable, row []string, headers ...string) []string {
	out := make([]string, len(headers))
	for i, h := range headers {
		out[i] = t.cell(row, h)
	}
	return out
}

func anyFilled(values []string) bool {
	for _, v := range values {
		if v != "" {
			return true
		}
	}
	return false
}
