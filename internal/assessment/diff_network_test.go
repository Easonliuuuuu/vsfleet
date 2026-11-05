package assessment

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func changedFieldNames(t *testing.T, kind string, before, after any, runtime bool) []string {
	t.Helper()
	marshal := func(v any) json.RawMessage {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var got []string
	for _, c := range changedResourceFields(kind, marshal(before), marshal(after), runtime) {
		got = append(got, c.Field)
	}
	return got
}

func TestHostNetworkChanges(t *testing.T) {
	yes, no, zero := true, false, int64(0)
	policy := vsphere.HostNICPolicy{TeamingPolicy: "loadbalance_srcid", Failback: &yes, ShapingAverageKbps: &zero, TCPSegmentation: &yes}
	lab := vsphere.Host{ID: "host-12", Name: "192.168.150.12",
		VSwitches:  []vsphere.HostVSwitch{{Key: "key-vim.host.VirtualSwitch-vSwitch0", Name: "vSwitch0", FreePorts: 1691, MTU: 1500, Uplinks: []string{"vmnic0"}, HostNICPolicy: policy}},
		PortGroups: []vsphere.HostPortGroup{{Name: "VM Network", Switch: "vSwitch0", TrafficShaping: &no, HostNICPolicy: policy}}}
	edit := func(f func(*vsphere.Host)) vsphere.Host {
		h := lab
		h.VSwitches = append([]vsphere.HostVSwitch(nil), lab.VSwitches...)
		h.PortGroups = append([]vsphere.HostPortGroup(nil), lab.PortGroups...)
		f(&h)
		return h
	}
	for _, tc := range []struct {
		name    string
		after   vsphere.Host
		runtime bool
		want    []string
	}{
		{"unchanged", lab, true, nil},
		{"teaming and uplinks reconfigured", edit(func(h *vsphere.Host) {
			h.VSwitches[0].TeamingPolicy, h.VSwitches[0].Uplinks = "failover_explicit", []string{"vmnic0", "vmnic2"}
			h.PortGroups[0].Failback = &no
		}), false, []string{"vswitches[vSwitch0].teaming_policy", "vswitches[vSwitch0].uplinks", "port_groups[VM Network].failback"}},
		{"free ports without runtime fields", edit(func(h *vsphere.Host) { h.VSwitches[0].FreePorts = 1600 }), false, nil},
		{"free ports with runtime fields", edit(func(h *vsphere.Host) { h.VSwitches[0].FreePorts = 1600 }), true, []string{"vswitches[vSwitch0].free_ports"}},
		// A capture predating inventory schema 36 recorded no policy.
		{"policy not recorded", edit(func(h *vsphere.Host) {
			h.VSwitches[0].HostNICPolicy, h.PortGroups[0].HostNICPolicy, h.PortGroups[0].TrafficShaping = vsphere.HostNICPolicy{}, vsphere.HostNICPolicy{}, nil
		}), true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := changedFieldNames(t, "host", lab, tc.after, tc.runtime); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("changed fields = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDVSwitchChanges(t *testing.T) {
	yes, no, avg := true, false, int64(100000)
	vms := 12
	shaping := vsphere.DVSTrafficShaping{IngressShaping: &no, IngressAverageKbps: &avg}
	port := vsphere.DVPortGroup{ID: "dvportgroup-1007", Key: "dvportgroup-1007", Name: "dvpg-mgmt", VLAN: "10", ReversePolicy: &yes, DVSTrafficShaping: shaping,
		FailureCriteria: &vsphere.DVSFailureCriteria{CheckSpeed: "minimum", SpeedMbps: 10}, Overrides: &vsphere.DVPortGroupOverrides{Blocked: true}}
	lab := vsphere.DVSwitch{ID: "dvs-1002", Name: "dvSwitch-Lab", ProductName: "DVS", VMCount: &vms, DVSTrafficShaping: shaping,
		PortGroups: []vsphere.DVPortGroup{port}}
	edit := func(f func(*vsphere.DVSwitch)) vsphere.DVSwitch {
		sw := lab
		sw.PortGroups = append([]vsphere.DVPortGroup(nil), lab.PortGroups...)
		f(&sw)
		return sw
	}
	more := 13
	for _, tc := range []struct {
		name    string
		after   vsphere.DVSwitch
		runtime bool
		want    []string
	}{
		{"unchanged", lab, true, nil},
		{"switch and port group reconfigured", edit(func(sw *vsphere.DVSwitch) {
			sw.LACPGroups = []vsphere.DVSLACPGroup{{Name: "lag1", Mode: "active"}}
			sw.PortGroups[0].VLAN = "20"
			sw.PortGroups[0].FailureCriteria = &vsphere.DVSFailureCriteria{CheckSpeed: "minimum", SpeedMbps: 100}
			sw.PortGroups[0].Overrides = &vsphere.DVPortGroupOverrides{Blocked: false}
		}), false, []string{"settings.lacp_groups", "port_groups[dvpg-mgmt].failure_criteria.speed_mbps", "port_groups[dvpg-mgmt].overrides.blocked", "port_groups[dvpg-mgmt].vlan"}},
		{"port group added", edit(func(sw *vsphere.DVSwitch) {
			sw.PortGroups = append(sw.PortGroups, vsphere.DVPortGroup{Key: "dvportgroup-1005", Name: "dvpg-vlan100"})
		}), false, []string{"port_groups"}},
		{"VMs moved without runtime fields", edit(func(sw *vsphere.DVSwitch) { sw.VMCount = &more }), false, nil},
		{"VMs moved with runtime fields", edit(func(sw *vsphere.DVSwitch) { sw.VMCount = &more }), true, []string{"settings.vm_count"}},
		// A capture predating inventory schema 36 recorded none of the
		// switch's or port group's new settings.
		{"policy not recorded", edit(func(sw *vsphere.DVSwitch) {
			sw.ProductName, sw.VMCount, sw.DVSTrafficShaping = "", nil, vsphere.DVSTrafficShaping{IngressShaping: &no}
			sw.PortGroups[0].ReversePolicy, sw.PortGroups[0].FailureCriteria, sw.PortGroups[0].Overrides = nil, nil, nil
			sw.PortGroups[0].DVSTrafficShaping = vsphere.DVSTrafficShaping{IngressShaping: &no}
		}), true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := changedFieldNames(t, "dvswitch", lab, tc.after, tc.runtime); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("changed fields = %v, want %v", got, tc.want)
			}
		})
	}
}
