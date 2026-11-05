package report

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// writeDVSwitchSheet renders one sheet of an export whose only distributed
// switch is sw.
func writeDVSwitchSheet(t *testing.T, sw vsphere.DVSwitch, sheet string) map[string]string {
	t.Helper()
	data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	payload, err := json.Marshal(sw)
	if err != nil {
		t.Fatal(err)
	}
	for i := range data.Resources {
		if data.Resources[i].Kind == "dvswitch" {
			data.Resources[i].Payload = payload
		}
	}
	return firstRowByHeader(t, data, sheet)
}

func ptrTo[T any](v T) *T { return &v }

// The "lab" values are what vCenter reports for the nested lab and what the
// real RVTools 4.8.2.1 export of it writes on 2026-11-05: vSwitch0 on
// 192.168.150.11, dvSwitch-Lab and its dvpg-mgmt port group. vCenter reports
// shaping in bit/s and bytes, which RVTools writes in Kbit/s and KB.
func TestWriteRVToolsNetworkPolicy(t *testing.T) {
	labNIC := vsphere.HostNICPolicy{TeamingPolicy: "loadbalance_srcid", ReversePolicy: ptrTo(true), NotifySwitches: ptrTo(true),
		Failback: ptrTo(true), ShapingAverageKbps: ptrTo(int64(0)), ShapingPeakKbps: ptrTo(int64(0)), ShapingBurstKB: ptrTo(int64(0)),
		ChecksumOffload: ptrTo(true), TCPSegmentation: ptrTo(true), ZeroCopyTransmit: ptrTo(true)}
	labNICCells := map[string]string{"Traffic Shaping": "FALSE", "Width": "0", "Peak": "0", "Burst": "0", "Policy": "loadbalance_srcid",
		"Reverse Policy": "TRUE", "Notify Switch": "TRUE", "Rolling Order": "FALSE", "Offload": "TRUE", "TSO": "TRUE", "Zero Copy Xmit": "TRUE"}
	labShaping := vsphere.DVSTrafficShaping{
		IngressShaping: ptrTo(false), IngressAverageKbps: ptrTo(int64(100000)), IngressPeakKbps: ptrTo(int64(100000)), IngressBurstKB: ptrTo(int64(102400)),
		EgressShaping: ptrTo(false), EgressAverageKbps: ptrTo(int64(100000)), EgressPeakKbps: ptrTo(int64(100000)), EgressBurstKB: ptrTo(int64(102400))}
	labShapingCells := map[string]string{"In Traffic Shaping": "FALSE", "In Avg": "100000", "In Peak": "100000", "In Burst": "102400",
		"Out Traffic Shaping": "FALSE", "Out Avg": "100000", "Out Peak": "100000", "Out Burst": "102400"}
	created := time.Date(2026, 9, 27, 16, 26, 7, 0, time.UTC)
	labPort := vsphere.DVPortGroup{ID: "dvportgroup-1007", Name: "dvpg-mgmt", TeamingPolicy: "loadbalance_srcmac", ReversePolicy: ptrTo(true),
		NotifySwitches: ptrTo(true), Failback: ptrTo(true), DVSTrafficShaping: labShaping,
		FailureCriteria: &vsphere.DVSFailureCriteria{CheckSpeed: "minimum", SpeedMbps: 10},
		Overrides:       &vsphere.DVPortGroupOverrides{Blocked: true, PortConfigResetAtDisconnect: true}}
	labSwitch := vsphere.DVSwitch{ID: "dvs-1002", Name: "dvSwitch-Lab", ProductName: "DVS", Created: &created, VMCount: ptrTo(12),
		Hosts: []string{"192.168.150.11", "192.168.150.12"}, LinkDiscoveryProtocol: "cdp", LinkDiscoveryOperation: "listen", MaxMTU: 1500,
		DVSTrafficShaping: labShaping, PortGroups: []vsphere.DVPortGroup{labPort}}

	merge := func(maps ...map[string]string) map[string]string {
		out := map[string]string{}
		for _, m := range maps {
			for k, v := range m {
				out[k] = v
			}
		}
		return out
	}
	blank := func(columns ...string) map[string]string {
		out := map[string]string{}
		for _, c := range columns {
			out[c] = ""
		}
		return out
	}
	policyColumns := []string{"Width", "Peak", "Burst", "Policy", "Reverse Policy", "Notify Switch", "Rolling Order", "Offload", "TSO", "Zero Copy Xmit"}

	t.Run("lab vSwitch", func(t *testing.T) {
		h := vsphere.Host{VSwitches: []vsphere.HostVSwitch{{Name: "vSwitch0", MTU: 1500, TrafficShaping: ptrTo(false), HostNICPolicy: labNIC}}}
		checkCells(t, writeHostSheet(t, h, "vSwitch"), merge(labNICCells, map[string]string{"Host": "esx-1", "Switch": "vSwitch0", "MTU": "1500"}))
	})
	t.Run("lab vPort", func(t *testing.T) {
		h := vsphere.Host{PortGroups: []vsphere.HostPortGroup{{Name: "VM Network", Switch: "vSwitch0", TrafficShaping: ptrTo(false), HostNICPolicy: labNIC}}}
		checkCells(t, writeHostSheet(t, h, "vPort"), merge(labNICCells, map[string]string{"Port Group": "VM Network", "VLAN": "0"}))
	})
	t.Run("vPort captured before schema 36", func(t *testing.T) {
		h := vsphere.Host{PortGroups: []vsphere.HostPortGroup{{Name: "VM Network", Switch: "vSwitch0"}}}
		checkCells(t, writeHostSheet(t, h, "vPort"), blank(append(policyColumns, "Traffic Shaping")...))
	})
	t.Run("lab dvSwitch", func(t *testing.T) {
		checkCells(t, writeDVSwitchSheet(t, labSwitch, "dvSwitch"), merge(labShapingCells, map[string]string{"Switch": "dvSwitch-Lab", "Name": "DVS",
			"Created": "2026/09/27 16:26:07", "Host members": "192.168.150.11, 192.168.150.12", "# VMs": "12", "CDP Type": "cdp",
			"CDP Operation": "listen", "LACP Name": "", "LACP Mode": "", "LACP Load Balance Alg.": "", "Max MTU": "1500", "Contact": "", "Admin Name": ""}))
	})
	t.Run("dvSwitch contact and LACP groups", func(t *testing.T) {
		sw := vsphere.DVSwitch{ID: "dvs-1", Name: "DVS-1", Contact: "Network team", ContactDetail: "netops@example.test",
			LACPGroups: []vsphere.DVSLACPGroup{{Name: "lag1", Mode: "active", LoadBalanceAlgorithm: "srcDestIpTcpUdpPortVlan"}, {Name: "lag2", Mode: "passive", LoadBalanceAlgorithm: "srcMac"}}}
		checkCells(t, writeDVSwitchSheet(t, sw, "dvSwitch"), map[string]string{"Admin Name": "Network team", "Contact": "netops@example.test",
			"LACP Name": "lag1, lag2", "LACP Mode": "active, passive", "LACP Load Balance Alg.": "srcDestIpTcpUdpPortVlan, srcMac"})
	})
	t.Run("dvSwitch captured before schema 36", func(t *testing.T) {
		sw := vsphere.DVSwitch{ID: "dvs-1", Name: "DVS-1", Hosts: []string{"esx-1"}}
		checkCells(t, writeDVSwitchSheet(t, sw, "dvSwitch"), merge(blank("Name", "Created", "# VMs", "In Traffic Shaping", "In Avg", "Out Burst"),
			map[string]string{"Host members": "esx-1"}))
	})
	t.Run("lab dvPort", func(t *testing.T) {
		checkCells(t, writeDVSwitchSheet(t, labSwitch, "dvPort"), merge(labShapingCells, map[string]string{"Port": "dvpg-mgmt", "Speed": "10",
			"Full Duplex": "FALSE", "Policy": "loadbalance_srcmac", "Reverse Policy": "TRUE", "Notify Switch": "TRUE", "Rolling Order": "FALSE",
			"Check Beacon": "FALSE", "Live Port Moving": "FALSE", "Check Duplex": "FALSE", "Check Error %": "FALSE", "Check Speed": "minimum",
			"Percentage": "0", "Block Override": "TRUE", "Config Reset": "TRUE", "Shaping Override": "FALSE", "Vendor Config Override": "FALSE",
			"Sec. Policy Override": "FALSE", "Teaming Override": "FALSE", "Vlan Override": "FALSE", "Object ID": "dvportgroup-1007"}))
	})
	t.Run("dvPort captured before schema 36", func(t *testing.T) {
		sw := vsphere.DVSwitch{ID: "dvs-1", Name: "DVS-1", PortGroups: []vsphere.DVPortGroup{{ID: "dvpg-1", Name: "frontend", TeamingPolicy: "loadbalance_srcid"}}}
		checkCells(t, writeDVSwitchSheet(t, sw, "dvPort"), merge(blank("Speed", "Full Duplex", "Reverse Policy", "Check Speed", "Percentage",
			"Block Override", "Config Reset", "In Avg"), map[string]string{"Policy": "loadbalance_srcid"}))
	})
}
