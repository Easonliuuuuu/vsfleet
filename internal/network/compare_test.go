package network

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func boolPtr(value bool) *bool { return &value }

func comparisonFixture(t *testing.T, sourcePGs, targetPGs []vsphere.DVPortGroup) assessment.ExportData {
	t.Helper()
	contexts := []assessment.ContextRun{
		{Name: "prod", VCenterID: "vc-prod", VMStatus: "success", Collections: completeCollections()},
		{Name: "dr", VCenterID: "vc-dr", VMStatus: "success", Collections: completeCollections()},
	}
	resources := make([]assessment.ResourceObservation, 0)
	add := func(context, vcenter, kind, id, name string, value any) {
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		resources = append(resources, assessment.ResourceObservation{Context: context, VCenterID: vcenter, Kind: kind, ID: id, Name: name, Payload: payload})
	}
	add("prod", "vc-prod", "cluster", "cluster-prod", "source", vsphere.Cluster{Location: vsphere.Location{Context: "prod", Datacenter: "dc"}, ID: "cluster-prod", Name: "source"})
	add("dr", "vc-dr", "cluster", "cluster-dr", "target", vsphere.Cluster{Location: vsphere.Location{Context: "dr", Datacenter: "dc"}, ID: "cluster-dr", Name: "target"})
	add("prod", "vc-prod", "host", "host-prod", "esx-prod", vsphere.Host{Location: vsphere.Location{Context: "prod", Datacenter: "dc"}, ID: "host-prod", Name: "esx-prod", Cluster: "source"})
	add("dr", "vc-dr", "host", "host-dr", "esx-dr", vsphere.Host{Location: vsphere.Location{Context: "dr", Datacenter: "dc"}, ID: "host-dr", Name: "esx-dr", Cluster: "target"})
	add("prod", "vc-prod", "dvswitch", "dvs-prod", "dvs-prod", vsphere.DVSwitch{Location: vsphere.Location{Context: "prod", Datacenter: "dc"}, ID: "dvs-prod", Name: "dvs-prod", UUID: "uuid-prod", MaxMTU: 1500, Hosts: []string{"host-prod"}, PortGroups: sourcePGs})
	add("dr", "vc-dr", "dvswitch", "dvs-dr", "dvs-dr", vsphere.DVSwitch{Location: vsphere.Location{Context: "dr", Datacenter: "dc"}, ID: "dvs-dr", Name: "dvs-dr", UUID: "uuid-dr", MaxMTU: 1500, Hosts: []string{"host-dr"}, PortGroups: targetPGs})
	return assessment.ExportData{Run: assessment.Run{ID: 42, InventorySchemaVersion: "12"}, Contexts: contexts, Resources: resources}
}

func completeCollections() []assessment.CollectionRun {
	return []assessment.CollectionRun{
		{Kind: "vm", Status: "success"},
		{Kind: "host", Status: "success"},
		{Kind: "cluster", Status: "success"},
		{Kind: "resourcepool", Status: "success"},
		{Kind: "dvswitch", Status: "success"},
		{Kind: "datastore", Status: "success"},
		{Kind: "network", Status: "success"},
	}
}

func portGroup(name, key, vlan string) vsphere.DVPortGroup {
	return vsphere.DVPortGroup{
		ID: "dvport-" + key, Key: key, Name: name, VLAN: vlan,
		TeamingPolicy: "loadbalance_srcid", Promiscuous: boolPtr(false), MACChanges: boolPtr(false), ForgedTransmits: boolPtr(false),
		ActiveUplinks: []string{"uplink1"},
	}
}

func TestCompareCleanMatchByVLAN(t *testing.T) {
	source := portGroup("app-prod", "pg-prod", "100")
	target := portGroup("app-dr", "pg-dr", "100")
	data := comparisonFixture(t, []vsphere.DVPortGroup{source}, []vsphere.DVPortGroup{target})
	result := Compare(data, "source", "target", nil)
	if result.Confidence != "complete" || len(result.Matched) != 1 || result.Matched[0].MatchBasis != "vlan" {
		t.Fatalf("unexpected comparison: %+v", result)
	}
	if len(result.MappingGaps) != 0 || len(result.TargetOnly) != 0 || len(result.Differences) != 0 {
		t.Fatalf("expected clean match: %+v", result)
	}
	if result.Source.HostCount != 1 || result.Target.HostCount != 1 {
		t.Fatalf("host counts were not resolved: %+v", result)
	}
}

func TestCompareReportsVLANGapAndAttachedVM(t *testing.T) {
	missing := portGroup("isolated", "pg-missing", "200")
	data := comparisonFixture(t, []vsphere.DVPortGroup{missing}, nil)
	vm := vsphere.VM{Location: vsphere.Location{Context: "prod"}, ID: "vm-1", Name: "app01", Cluster: "source", NICs: []vsphere.VMNIC{{Network: "isolated", NetworkID: "pg-missing", SwitchID: "uuid-prod"}}}
	data.VMs = []assessment.ExportVM{{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-prod", VM: vm}}}
	result := Compare(data, "source", "target", nil)
	if result.Confidence != "complete" || len(result.MappingGaps) != 1 {
		t.Fatalf("unexpected gap comparison: %+v", result)
	}
	gap := result.MappingGaps[0]
	if len(gap.AttachedVMs) != 1 || gap.AttachedVMs[0].Name != "app01" || gap.Severity != severityBlocker {
		t.Fatalf("attached VM was not resolved: %+v", gap)
	}
	readiness := Verdict(result)
	if readiness.Verdict != "blocked" || len(readiness.Blockers) != 1 {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
}

func TestCompareReportsMTUMismatch(t *testing.T) {
	source := portGroup("app", "pg-prod", "100")
	target := portGroup("app", "pg-dr", "100")
	data := comparisonFixture(t, []vsphere.DVPortGroup{source}, []vsphere.DVPortGroup{target})
	for index := range data.Resources {
		if data.Resources[index].Kind != "dvswitch" || data.Resources[index].Context != "dr" {
			continue
		}
		var sw vsphere.DVSwitch
		if !assessment.DecodeResource(data.Resources[index], &sw) {
			t.Fatal("could not decode test switch")
		}
		sw.MaxMTU = 9000
		data.Resources[index].Payload, _ = json.Marshal(sw)
	}
	result := Compare(data, "source", "target", nil)
	if len(result.Differences) != 1 || result.Differences[0].Field != "mtu" || result.Differences[0].Severity != severityBlocker {
		t.Fatalf("unexpected MTU differences: %+v", result.Differences)
	}
	if Verdict(result).Verdict != "blocked" {
		t.Fatalf("MTU mismatch should block: %+v", Verdict(result))
	}
}

func TestCompareBlindDistributedSwitchIsUnknown(t *testing.T) {
	data := comparisonFixture(t, []vsphere.DVPortGroup{portGroup("app", "pg-prod", "100")}, []vsphere.DVPortGroup{portGroup("app", "pg-dr", "100")})
	for index := range data.Contexts {
		if data.Contexts[index].Name == "dr" {
			for collection := range data.Contexts[index].Collections {
				if data.Contexts[index].Collections[collection].Kind == "dvswitch" {
					data.Contexts[index].Collections[collection].Status = "failed"
				}
			}
		}
	}
	result := Compare(data, "source", "target", nil)
	if result.Confidence != "unknown" || len(result.Blind) != 1 || result.Blind[0].Context != "dr" {
		t.Fatalf("blind switch should be unknown: %+v", result)
	}
	if len(result.Differences) != 0 {
		t.Fatalf("clean evidence should not produce differences: %+v", result.Differences)
	}
}

func TestCompareAmbiguousClusterName(t *testing.T) {
	data := comparisonFixture(t, nil, nil)
	data.Resources = append(data.Resources,
		assessment.ResourceObservation{Context: "dr", VCenterID: "vc-dr", Kind: "cluster", ID: "cluster-source-2", Name: "source", Payload: mustJSON(t, vsphere.Cluster{Location: vsphere.Location{Context: "dr"}, ID: "cluster-source-2", Name: "source"})},
	)
	result := Compare(data, "source", "target", nil)
	if !result.Ambiguous || len(result.Candidates) != 2 || result.Confidence != "unknown" {
		t.Fatalf("expected ambiguity: %+v", result)
	}
}

func TestCompareIsDeterministic(t *testing.T) {
	fixture := func() assessment.ExportData {
		t.Helper()
		data := comparisonFixture(t,
			[]vsphere.DVPortGroup{portGroup("z-app", "pg-z-prod", "100"), portGroup("a-app", "pg-a-prod", "110"), portGroup("isolated", "pg-gap", "200")},
			[]vsphere.DVPortGroup{portGroup("z-app", "pg-z-dr", "100"), portGroup("a-app", "pg-a-dr", "110")})
		for i := range data.Resources {
			r := &data.Resources[i]
			switch r.Kind {
			case "host":
				var host vsphere.Host
				if !assessment.DecodeResource(*r, &host) {
					t.Fatal("decode fixture host")
				}
				host.PortGroups = []vsphere.HostPortGroup{{Name: "z-standard", Switch: "vSwitch0", VLAN: 300}, {Name: "b-standard", Switch: "vSwitch0", VLAN: 400}}
				r.Payload = mustJSON(t, host)
				host.ID += "-2"
				host.Name += "-2"
				data.Resources = append(data.Resources, assessment.ResourceObservation{Context: r.Context, VCenterID: r.VCenterID, Kind: "host", ID: host.ID, Name: host.Name, Payload: mustJSON(t, host)})
			case "dvswitch":
				var sw vsphere.DVSwitch
				if !assessment.DecodeResource(*r, &sw) {
					t.Fatal("decode fixture switch")
				}
				sw.Hosts = append(sw.Hosts, sw.Hosts[0]+"-2")
				r.Payload = mustJSON(t, sw)
			}
		}
		for _, name := range []string{"z-vm", "a-vm"} {
			vm := vsphere.VM{ID: name, Name: name, Cluster: "source", NICs: []vsphere.VMNIC{{Network: "isolated", NetworkID: "pg-gap", SwitchID: "uuid-prod"}}}
			data.VMs = append(data.VMs, assessment.ExportVM{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-prod", VM: vm}})
		}
		return data
	}
	left, right := fixture(), fixture()
	slices.Reverse(right.Resources)
	slices.Reverse(right.Contexts)
	slices.Reverse(right.VMs)
	for i := range right.Resources {
		r := &right.Resources[i]
		switch r.Kind {
		case "host":
			var host vsphere.Host
			if !assessment.DecodeResource(*r, &host) {
				t.Fatal("decode fixture host")
			}
			slices.Reverse(host.PortGroups)
			r.Payload = mustJSON(t, host)
		case "dvswitch":
			var sw vsphere.DVSwitch
			if !assessment.DecodeResource(*r, &sw) {
				t.Fatal("decode fixture switch")
			}
			slices.Reverse(sw.PortGroups)
			slices.Reverse(sw.Hosts)
			r.Payload = mustJSON(t, sw)
		}
	}
	result := Compare(left, "source", "target", nil)
	if result.Confidence != "complete" || result.Source.HostCount != 2 || result.Target.HostCount != 2 || len(result.Matched) != 4 || len(result.MappingGaps) != 1 || len(result.MappingGaps[0].AttachedVMs) != 2 {
		t.Fatalf("determinism fixture missed comparison paths: %+v", result)
	}
	if !reflect.DeepEqual(result, Compare(right, "source", "target", nil)) {
		t.Fatal("comparison changed with input ordering")
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestFullCoverageOnClustersOfDifferentSizesIsNoDifference(t *testing.T) {
	source := NetworkSummary{Name: "web", VLAN: "110", CoveredHosts: 16, TotalHosts: 16}
	target := NetworkSummary{Name: "web", VLAN: "110", CoveredHosts: 6, TotalHosts: 6}
	if got := Differences(NetworkMatch{Source: source, Target: target, MatchBasis: "vlan"}); len(got) != 0 {
		t.Fatalf("full coverage on both sides reported %+v", got)
	}
	target.CoveredHosts = 5
	got := Differences(NetworkMatch{Source: source, Target: target, MatchBasis: "vlan"})
	if len(got) != 1 || got[0].Field != "host_coverage" || !IsBlocker(got[0].Severity) {
		t.Fatalf("a target host without the network is not a host_coverage blocker: %+v", got)
	}
}

// Cluster refs carry no ID, and the clusters they come from are gathered by
// map iteration; refs that tie on name, context and vCenter must still sort
// one way.
func TestSortClusterRefsIsIndependentOfInputOrder(t *testing.T) {
	a := ClusterRef{Name: "Prod", Context: "vc", VCenterID: "uuid", HostCount: 2, Resolved: true}
	b := ClusterRef{Name: "Prod", Context: "vc", VCenterID: "uuid", HostCount: 5, Resolved: true}
	first, second := []ClusterRef{a, b}, []ClusterRef{b, a}
	sortClusterRefs(first)
	sortClusterRefs(second)
	if first[0] != second[0] || first[1] != second[1] {
		t.Fatalf("order depends on input: %+v vs %+v", first, second)
	}
}
