package report

import (
	"cmp"
	"encoding/json"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// nicPolicyHeaders are the teaming, shaping and offload columns vSwitch and
// vPort share, in RVTools' order.
var nicPolicyHeaders = []string{"Traffic Shaping", "Width", "Peak", "Burst", "Policy", "Reverse Policy", "Notify Switch", "Rolling Order",
	"Offload", "TSO", "Zero Copy Xmit"}

// switchHeaders and portHeaders follow RVTools' vSwitch and vPort order;
// Uplinks is vsfleet's own, after the last RVTools column.
var (
	switchHeaders = hostLeadHeaders(append(append([]string{"Switch", "# Ports", "Free Ports", "Promiscuous Mode", "Mac Changes",
		"Forged Transmits"}, nicPolicyHeaders...), "MTU", "Uplinks")...)
	portHeaders = hostLeadHeaders(append([]string{"Port Group", "Switch", "VLAN", "Promiscuous Mode", "Mac Changes", "Forged Transmits"},
		nicPolicyHeaders...)...)
)

// shapingHeaders are dvSwitch's and dvPort's In and Out shaping columns.
var shapingHeaders = []string{"In Traffic Shaping", "In Avg", "In Peak", "In Burst", "Out Traffic Shaping", "Out Avg", "Out Peak", "Out Burst"}

// dvSwitchHeaders and dvPortHeaders follow RVTools' dvSwitch and dvPort
// order. vsfleet's own columns follow the RVTools column they extend, and
// dvPort keeps the switch's Datacenter, which RVTools leaves out.
var (
	dvSwitchHeaders = append(append([]string{"Switch", "Datacenter", "Name", "Vendor", "Version", "UUID", "Description", "Created",
		"Host members", "Max Ports", "# Ports", "# VMs"}, shapingHeaders...), "CDP Type", "CDP Operation", "LACP Name", "LACP Mode",
		"LACP Load Balance Alg.", "LACP version", "Max MTU", "Contact", "Admin Name", "Uplink ports", "Object ID", "VI SDK Server",
		"VI SDK UUID", "vsfleet Context")
	dvPortHeaders = append(append([]string{"Port", "Switch", "Key", "Type", "Backing type", "# Ports", "VLAN", "Uplink", "Speed",
		"Full Duplex", "Blocked", "Auto expand", "Allow Promiscuous", "Mac Changes", "Active Uplink", "Standby Uplink", "Policy",
		"Forged Transmits"}, shapingHeaders...), "Reverse Policy", "Notify Switch", "Rolling Order", "Check Beacon", "Live Port Moving",
		"Check Duplex", "Check Error %", "Check Speed", "Percentage", "Block Override", "Config Reset", "Shaping Override",
		"Vendor Config Override", "Sec. Policy Override", "Teaming Override", "Vlan Override", "Logical switch UUID", "Segment ID",
		"Datacenter", "Object ID", "VI SDK Server", "VI SDK UUID", "vsfleet Context")
)

// dvSwitchDateHeaders are the dvSwitch columns written as date cells.
var dvSwitchDateHeaders = []string{"Created"}

func nicPolicyCells(shaping *bool, p vsphere.HostNICPolicy) []any {
	return []any{optionalBool(shaping), optionalInt64(p.ShapingAverageKbps), optionalInt64(p.ShapingPeakKbps), optionalInt64(p.ShapingBurstKB),
		optionalString(p.TeamingPolicy), optionalBool(p.ReversePolicy), optionalBool(p.NotifySwitches), invertedOptionalBool(p.Failback),
		optionalBool(p.ChecksumOffload), optionalBool(p.TCPSegmentation), optionalBool(p.ZeroCopyTransmit)}
}

func shapingCells(s vsphere.DVSTrafficShaping) []any {
	return []any{optionalBool(s.IngressShaping), optionalInt64(s.IngressAverageKbps), optionalInt64(s.IngressPeakKbps), optionalInt64(s.IngressBurstKB),
		optionalBool(s.EgressShaping), optionalInt64(s.EgressAverageKbps), optionalInt64(s.EgressPeakKbps), optionalInt64(s.EgressBurstKB)}
}

func switchRows(data assessment.ExportData) [][]any {
	rows := make([][]any, 0)
	for _, item := range hostConfigResources(data) {
		for _, sw := range item.host.VSwitches {
			row := []any{sw.Name, sw.NumPorts, sw.FreePorts, optionalBool(sw.Promiscuous), optionalBool(sw.MACChanges), optionalBool(sw.ForgedTransmits)}
			row = append(append(row, nicPolicyCells(sw.TrafficShaping, sw.HostNICPolicy)...), sw.MTU, optionalString(strings.Join(sw.Uplinks, ", ")))
			rows = append(rows, hostLeadRow(hostTail(data, item.resource, item.host), row))
		}
	}
	return rows
}

func portRows(data assessment.ExportData) [][]any {
	rows := make([][]any, 0)
	for _, item := range hostConfigResources(data) {
		for _, port := range item.host.PortGroups {
			row := []any{port.Name, port.Switch, port.VLAN, optionalBool(port.Promiscuous), optionalBool(port.MACChanges), optionalBool(port.ForgedTransmits)}
			row = append(row, nicPolicyCells(port.TrafficShaping, port.HostNICPolicy)...)
			rows = append(rows, hostLeadRow(hostTail(data, item.resource, item.host), row))
		}
	}
	return rows
}

// dvSwitches decodes each captured distributed switch with its observation.
func dvSwitches(data assessment.ExportData) []struct {
	resource assessment.ResourceObservation
	sw       vsphere.DVSwitch
} {
	var out []struct {
		resource assessment.ResourceObservation
		sw       vsphere.DVSwitch
	}
	for _, resource := range data.Resources {
		if resource.Kind != "dvswitch" {
			continue
		}
		var sw vsphere.DVSwitch
		if err := json.Unmarshal(resource.Payload, &sw); err != nil {
			continue
		}
		out = append(out, struct {
			resource assessment.ResourceObservation
			sw       vsphere.DVSwitch
		}{resource, sw})
	}
	return out
}

// lacpCells joins the switch's link aggregation groups' names, modes and
// load-balancing algorithms, each comma-separated in group order.
func lacpCells(groups []vsphere.DVSLACPGroup) []any {
	names, modes, algorithms := make([]string, len(groups)), make([]string, len(groups)), make([]string, len(groups))
	for i, g := range groups {
		names[i], modes[i], algorithms[i] = g.Name, g.Mode, g.LoadBalanceAlgorithm
	}
	return []any{optionalString(strings.Join(names, ", ")), optionalString(strings.Join(modes, ", ")), optionalString(strings.Join(algorithms, ", "))}
}

func dvSwitchRows(data assessment.ExportData) [][]any {
	rows := make([][]any, 0)
	for _, item := range dvSwitches(data) {
		sw, resource := item.sw, item.resource
		var vms any
		if sw.VMCount != nil {
			vms = *sw.VMCount
		}
		row := []any{cmp.Or(sw.Name, resource.Name), sw.Datacenter, optionalString(sw.ProductName), sw.Vendor, sw.Version, sw.UUID,
			optionalString(sw.Description), optionalTime(sw.Created), optionalString(strings.Join(sw.Hosts, ", ")), sw.MaxPorts, sw.NumPorts, vms}
		row = append(row, shapingCells(sw.DVSTrafficShaping)...)
		row = append(row, optionalString(sw.LinkDiscoveryProtocol), optionalString(sw.LinkDiscoveryOperation))
		row = append(row, lacpCells(sw.LACPGroups)...)
		rows = append(rows, append(row, optionalString(sw.LACPVersion), sw.MaxMTU, optionalString(sw.ContactDetail), optionalString(sw.Contact),
			optionalString(strings.Join(sw.UplinkPorts, ", ")), cmp.Or(sw.ID, resource.ID), sdkServer(data, resource.Context),
			resource.VCenterID, resource.Context))
	}
	return rows
}

func dvPortRows(data assessment.ExportData) [][]any {
	rows := make([][]any, 0)
	for _, item := range dvSwitches(data) {
		sw, resource := item.sw, item.resource
		for _, port := range sw.PortGroups {
			c := port.FailureCriteria
			o := port.Overrides
			row := []any{port.Name, cmp.Or(sw.Name, resource.Name), port.Key, port.Type, port.BackingType, port.NumPorts,
				optionalString(port.VLAN), port.Uplink, failureCell(c, func(c vsphere.DVSFailureCriteria) any { return c.SpeedMbps }),
				failureCell(c, func(c vsphere.DVSFailureCriteria) any { return c.FullDuplex }), optionalBool(port.Blocked),
				optionalBool(port.AutoExpand), optionalBool(port.Promiscuous), optionalBool(port.MACChanges),
				optionalString(strings.Join(port.ActiveUplinks, ", ")), optionalString(strings.Join(port.StandbyUplinks, ", ")),
				optionalString(port.TeamingPolicy), optionalBool(port.ForgedTransmits)}
			row = append(row, shapingCells(port.DVSTrafficShaping)...)
			row = append(row, optionalBool(port.ReversePolicy), optionalBool(port.NotifySwitches), invertedOptionalBool(port.Failback),
				failureCell(c, func(c vsphere.DVSFailureCriteria) any { return c.CheckBeacon }),
				overrideCell(o, func(o vsphere.DVPortGroupOverrides) bool { return o.LivePortMoving }),
				failureCell(c, func(c vsphere.DVSFailureCriteria) any { return c.CheckDuplex }),
				failureCell(c, func(c vsphere.DVSFailureCriteria) any { return c.CheckErrorPercent }),
				failureCell(c, func(c vsphere.DVSFailureCriteria) any { return optionalString(c.CheckSpeed) }),
				failureCell(c, func(c vsphere.DVSFailureCriteria) any { return c.ErrorPercent }),
				overrideCell(o, func(o vsphere.DVPortGroupOverrides) bool { return o.Blocked }),
				overrideCell(o, func(o vsphere.DVPortGroupOverrides) bool { return o.PortConfigResetAtDisconnect }),
				overrideCell(o, func(o vsphere.DVPortGroupOverrides) bool { return o.Shaping }),
				overrideCell(o, func(o vsphere.DVPortGroupOverrides) bool { return o.VendorConfig }),
				overrideCell(o, func(o vsphere.DVPortGroupOverrides) bool { return o.SecurityPolicy }),
				overrideCell(o, func(o vsphere.DVPortGroupOverrides) bool { return o.UplinkTeaming }),
				overrideCell(o, func(o vsphere.DVPortGroupOverrides) bool { return o.VLAN }),
				optionalString(port.LogicalSwitchUUID), optionalString(port.SegmentID),
				sw.Datacenter, cmp.Or(port.ID, resource.ID), sdkServer(data, resource.Context), resource.VCenterID, resource.Context)
			rows = append(rows, row)
		}
	}
	return rows
}

// failureCell is one failure-criteria cell, empty when the port group
// recorded no failure criteria.
func failureCell(c *vsphere.DVSFailureCriteria, value func(vsphere.DVSFailureCriteria) any) any {
	if c == nil {
		return nil
	}
	return value(*c)
}

// overrideCell is one override cell, empty when the port group recorded no
// overrides.
func overrideCell(o *vsphere.DVPortGroupOverrides, value func(vsphere.DVPortGroupOverrides) bool) any {
	if o == nil {
		return nil
	}
	return value(*o)
}
