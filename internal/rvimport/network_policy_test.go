package rvimport

import (
	"reflect"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The nested lab's network policy, as vCenter reports it and RVTools 4.8.2.1
// exports it on 2026-11-05. The lab has no LACP group and no switch contact,
// so those are synthetic.
var (
	labNICPolicy = vsphere.HostNICPolicy{TeamingPolicy: "loadbalance_srcid", ReversePolicy: ptr(true), NotifySwitches: ptr(true),
		Failback: ptr(true), ShapingAverageKbps: ptr(int64(0)), ShapingPeakKbps: ptr(int64(0)), ShapingBurstKB: ptr(int64(0)),
		ChecksumOffload: ptr(true), TCPSegmentation: ptr(true), ZeroCopyTransmit: ptr(true)}
	labShaping = vsphere.DVSTrafficShaping{
		IngressShaping: ptr(false), IngressAverageKbps: ptr(int64(100000)), IngressPeakKbps: ptr(int64(100000)), IngressBurstKB: ptr(int64(102400)),
		EgressShaping: ptr(false), EgressAverageKbps: ptr(int64(100000)), EgressPeakKbps: ptr(int64(100000)), EgressBurstKB: ptr(int64(102400))}
	labDVSCreated = time.Date(2026, 9, 27, 16, 26, 7, 0, time.UTC)
	labDVPort     = vsphere.DVPortGroup{ID: "dvportgroup-1007", Key: "dvportgroup-1007", Name: "dvpg-mgmt", Switch: "dvSwitch-Lab",
		TeamingPolicy: "loadbalance_srcmac", ReversePolicy: ptr(true), DVSTrafficShaping: labShaping,
		FailureCriteria: &vsphere.DVSFailureCriteria{CheckSpeed: "minimum", SpeedMbps: 10},
		Overrides:       &vsphere.DVPortGroupOverrides{Blocked: true, PortConfigResetAtDisconnect: true}}
)

func networkPolicyFixture(t *testing.T) string {
	t.Helper()
	return writeFixtureWorkbook(t, func(data *assessment.ExportData) {
		data.Run.InventorySchemaVersion = assessment.CurrentInventorySchemaVersion
		editHost(t, data, "host-a1", func(h *vsphere.Host) {
			h.VSwitches = []vsphere.HostVSwitch{{Name: "vSwitch0", MTU: 1500, TrafficShaping: ptr(false), HostNICPolicy: labNICPolicy}}
			h.PortGroups = []vsphere.HostPortGroup{{Name: "VM Network", Switch: "vSwitch0", TrafficShaping: ptr(false), HostNICPolicy: labNICPolicy}}
		})
		data.Resources = append(data.Resources, dvsResource(t, "alpha", "vc-alpha-uuid", vsphere.DVSwitch{
			Location: vsphere.Location{Context: "alpha", Datacenter: "dc-a"}, ID: "dvs-1002", Name: "dvSwitch-Lab",
			Contact: "Network team", ContactDetail: "netops@example.test", ProductName: "DVS", Created: &labDVSCreated, VMCount: ptr(12),
			LACPGroups:        []vsphere.DVSLACPGroup{{Name: "lag1", Mode: "active", LoadBalanceAlgorithm: "srcDestIpTcpUdpPortVlan"}, {Name: "lag2", Mode: "passive", LoadBalanceAlgorithm: "srcMac"}},
			DVSTrafficShaping: labShaping, PortGroups: []vsphere.DVPortGroup{labDVPort},
		}))
	})
}

func importedDVSwitch(t *testing.T, result *Result) *vsphere.DVSwitch {
	t.Helper()
	for _, c := range result.contexts {
		for _, sw := range c.dvswitches {
			if sw.ID == "dvs-1002" {
				return sw
			}
		}
	}
	t.Fatal("dvSwitch-Lab not imported")
	return nil
}

func TestNetworkPolicyColumnsAreReadBack(t *testing.T) {
	result, err := Parse(openFixture(t, networkPolicyFixture(t)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	h := findHost(result, "alpha", "esx-a1")
	if h == nil || len(h.VSwitches) != 1 || len(h.PortGroups) != 1 {
		t.Fatalf("esx-a1 switches not imported: %+v", h)
	}
	if got := h.VSwitches[0].HostNICPolicy; !reflect.DeepEqual(got, labNICPolicy) {
		t.Errorf("vSwitch policy = %+v\nwant %+v", got, labNICPolicy)
	}
	if got := h.PortGroups[0]; !reflect.DeepEqual(got.HostNICPolicy, labNICPolicy) || got.TrafficShaping == nil || *got.TrafficShaping {
		t.Errorf("vPort policy = %+v", got)
	}

	sw := importedDVSwitch(t, result)
	if sw.Contact != "Network team" || sw.ContactDetail != "netops@example.test" || sw.ProductName != "DVS" ||
		sw.VMCount == nil || *sw.VMCount != 12 || sw.Created == nil || !sw.Created.Equal(labDVSCreated) {
		t.Errorf("dvSwitch = contact %q / %q, product %q, VMs %v, created %v", sw.Contact, sw.ContactDetail, sw.ProductName, sw.VMCount, sw.Created)
	}
	wantLACP := []vsphere.DVSLACPGroup{{Name: "lag1", Mode: "active", LoadBalanceAlgorithm: "srcDestIpTcpUdpPortVlan"}, {Name: "lag2", Mode: "passive", LoadBalanceAlgorithm: "srcMac"}}
	if !reflect.DeepEqual(sw.LACPGroups, wantLACP) || !reflect.DeepEqual(sw.DVSTrafficShaping, labShaping) {
		t.Errorf("dvSwitch LACP %+v, shaping %+v", sw.LACPGroups, sw.DVSTrafficShaping)
	}
	if len(sw.PortGroups) != 1 {
		t.Fatalf("dvPort rows = %+v", sw.PortGroups)
	}
	p := sw.PortGroups[0]
	if p.TeamingPolicy != labDVPort.TeamingPolicy || !reflect.DeepEqual(p.ReversePolicy, labDVPort.ReversePolicy) ||
		!reflect.DeepEqual(p.DVSTrafficShaping, labShaping) || !reflect.DeepEqual(p.FailureCriteria, labDVPort.FailureCriteria) ||
		!reflect.DeepEqual(p.Overrides, labDVPort.Overrides) {
		t.Errorf("dvPort = %+v\nfailure %+v, overrides %+v", p, p.FailureCriteria, p.Overrides)
	}
}

// Before inventory schema 36 vsfleet's dvSwitch wrote the administrator's
// name as Contact and the details as Contact detail, where RVTools' Contact
// is the details. Such a workbook keeps its meaning.
func TestOlderDVSwitchContactColumnsAreRead(t *testing.T) {
	f := openFixture(t, networkPolicyFixture(t))
	for current, older := range map[string]string{"Contact": "Contact detail", "Admin Name": "Contact"} {
		setColumnValue(t, f, sheetDVSwitch, current, 1, older)
	}
	result, err := Parse(f, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sw := importedDVSwitch(t, result); sw.Contact != "Network team" || sw.ContactDetail != "netops@example.test" {
		t.Errorf("contact = %q, detail = %q", sw.Contact, sw.ContactDetail)
	}
}

// A port group captured before inventory schema 36 is exported with blank
// failure-detection and override cells, which read back as not recorded.
func TestBlankDVPortPolicyCellsStayUnrecorded(t *testing.T) {
	path := writeFixtureWorkbook(t, func(data *assessment.ExportData) {
		data.Resources = append(data.Resources, dvsResource(t, "alpha", "vc-alpha-uuid", vsphere.DVSwitch{
			Location: vsphere.Location{Context: "alpha", Datacenter: "dc-a"}, ID: "dvs-1002", Name: "dvSwitch-Lab",
			PortGroups: []vsphere.DVPortGroup{{ID: "dvportgroup-1007", Key: "dvportgroup-1007", Name: "dvpg-mgmt", TeamingPolicy: "loadbalance_srcid"}},
		}))
	})
	result, err := Parse(openFixture(t, path), Options{})
	if err != nil {
		t.Fatal(err)
	}
	sw := importedDVSwitch(t, result)
	if len(sw.PortGroups) != 1 {
		t.Fatalf("dvPort rows = %+v", sw.PortGroups)
	}
	if p := sw.PortGroups[0]; p.FailureCriteria != nil || p.Overrides != nil || p.ReversePolicy != nil {
		t.Errorf("failure criteria %+v, overrides %+v, reverse policy %v; want all unrecorded", p.FailureCriteria, p.Overrides, p.ReversePolicy)
	}
}
