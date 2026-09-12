package topology

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func topologyResource(t *testing.T, contextName, vcenter, kind, id, name string, value any) assessment.ResourceObservation {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return assessment.ResourceObservation{Context: contextName, VCenterID: vcenter, Kind: kind, ID: id, Name: name, Payload: payload}
}

func topologyContext(name string, vmStatus string, network bool) assessment.ContextRun {
	if vmStatus == "" {
		vmStatus = "success"
	}
	kinds := []string{"vm", "host", "cluster", "datastore", "resourcepool", "dvswitch"}
	if network {
		kinds = append(kinds, "network")
	}
	collections := make([]assessment.CollectionRun, 0, len(kinds))
	for _, kind := range kinds {
		status := "success"
		if vmStatus == "failed" && kind == "vm" {
			status = "failed"
		}
		collections = append(collections, assessment.CollectionRun{Kind: kind, Status: status, Error: map[bool]string{true: "proxy connection refused", false: ""}[status == "failed"]})
	}
	return assessment.ContextRun{Name: name, VCenterID: "vc-" + name, VMStatus: vmStatus, Collections: collections}
}

func datastoreSubjectData(t *testing.T, schema string, secondName string, blindSecond bool) assessment.ExportData {
	first := vsphere.Datastore{Location: vsphere.Location{Context: "prod", Datacenter: "dc"}, ID: "ds-prod", Name: "ds-prod", Backing: vsphere.DatastoreBacking{Extents: []string{"naa.shared"}}}
	second := vsphere.Datastore{Location: vsphere.Location{Context: "edge", Datacenter: "dc"}, ID: "ds-edge", Name: secondName, Backing: vsphere.DatastoreBacking{Extents: []string{"naa.shared"}}}
	contexts := []assessment.ContextRun{topologyContext("prod", "success", schema >= "12"), topologyContext("edge", "failed", schema >= "12")}
	if !blindSecond {
		contexts[1].VMStatus = "success"
		for i := range contexts[1].Collections {
			if contexts[1].Collections[i].Kind == "vm" {
				contexts[1].Collections[i].Status = "success"
			}
		}
	}
	data := assessment.ExportData{
		Run: assessment.Run{ID: 7, InventorySchemaVersion: schema}, Contexts: contexts,
		VMs: []assessment.ExportVM{
			{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-prod", VM: vsphere.VM{ID: "vm-prod", Name: "app-prod", Disks: []vsphere.VMDisk{{BackingPath: "[ds-prod] app/disk.vmdk"}}}}},
			{Observation: assessment.Observation{Context: "edge", VCenterID: "vc-edge", VM: vsphere.VM{ID: "vm-edge", Name: "app-edge", Disks: []vsphere.VMDisk{{BackingPath: "[" + secondName + "] app/disk.vmdk"}}}}},
		},
		Resources: []assessment.ResourceObservation{
			topologyResource(t, "prod", "vc-prod", "datastore", first.ID, first.Name, first),
			topologyResource(t, "edge", "vc-edge", "datastore", second.ID, second.Name, second),
		},
	}
	if blindSecond {
		data.VMs = data.VMs[:1]
	}
	return data
}

func TestSharedDatastoreIdentityCrossContextBlastRadius(t *testing.T) {
	data := datastoreSubjectData(t, "12", "ds-edge", false)
	graph := Build(data)
	subjects := graph.Resolve(KindDatastore, "ds-prod", nil)
	if len(subjects) != 1 || len(subjects[0].Members) != 2 {
		t.Fatalf("subjects=%+v, want one subject with two members", subjects)
	}
	result := graph.BlastRadius(subjects[0], 1)
	if result.Confidence != ConfidenceComplete || len(result.Edges) != 2 {
		t.Fatalf("result=%+v, want two complete VM edges", result)
	}
	seen := map[string]bool{}
	for _, edge := range result.Edges {
		seen[edge.From.Name] = true
	}
	if !seen["app-prod"] || !seen["app-edge"] {
		t.Fatalf("blast edges=%+v", result.Edges)
	}
}

func TestSameNameIndependentDatastoresAreAmbiguous(t *testing.T) {
	data := datastoreSubjectData(t, "12", "datastore1", false)
	data.Resources[0].Name = "datastore1"
	var first vsphere.Datastore
	_ = json.Unmarshal(data.Resources[0].Payload, &first)
	first.Name = "datastore1"
	first.Backing.Extents = []string{"naa.left"}
	data.Resources[0].Payload, _ = json.Marshal(first)
	var second vsphere.Datastore
	_ = json.Unmarshal(data.Resources[1].Payload, &second)
	second.Name = "datastore1"
	second.Backing.Extents = []string{"naa.right"}
	data.Resources[1].Payload, _ = json.Marshal(second)
	if got := len(Build(data).Resolve(KindDatastore, "datastore1", nil)); got != 2 {
		t.Fatalf("subjects=%d, want 2", got)
	}
}

func TestMemberlessSubjectCannotResolveByNameAcrossContexts(t *testing.T) {
	data := datastoreSubjectData(t, "12", "datastore1", false)
	data.Resources[0].Name = "datastore1"
	data.Resources[1].Name = "datastore1"
	graph := Build(data)
	result := graph.Topology(Subject{Kind: string(KindDatastore), Name: "datastore1"})
	if result.Confidence != ConfidenceUnknown {
		t.Fatalf("member-less subject confidence=%s, want unknown", result.Confidence)
	}
	if len(result.Subject.Members) != 0 || len(result.Ancestors) != 0 || len(result.Edges) != 0 {
		t.Fatalf("member-less subject resolved graph data: %+v", result)
	}
}

func TestUnknownReportsBlindnessOnlyForRequestedScope(t *testing.T) {
	data := datastoreSubjectData(t, "12", "ds-edge", true)
	graph := Build(data)
	result := graph.Unknown(KindDatastore, "missing", []string{"edge"})
	if result.Confidence != ConfidenceUnknown || len(result.Subject.Members) != 0 {
		t.Fatalf("unknown result=%+v", result)
	}
	if len(result.Blind) != 1 || result.Blind[0].Context != "edge" {
		t.Fatalf("unknown blindness=%+v, want only edge", result.Blind)
	}
}

func TestBlindContextDowngradesButKeepsFoundEdges(t *testing.T) {
	data := datastoreSubjectData(t, "12", "ds-edge", true)
	graph := Build(data)
	subject := graph.Resolve(KindDatastore, "ds-prod", nil)[0]
	result := graph.BlastRadius(subject, 1)
	if result.Confidence != ConfidencePartial || len(result.Edges) != 1 {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Blind) != 1 || result.Blind[0].Context != "edge" || !strings.Contains(result.Blind[0].Reason, "vm collection") {
		t.Fatalf("blind=%+v", result.Blind)
	}
}

func TestVMDependenciesPreferNetworkIdentityJoin(t *testing.T) {
	dvs := vsphere.DVSwitch{Location: vsphere.Location{Context: "prod", Datacenter: "dc"}, ID: "dvs-1", Name: "switch", UUID: "switch-uuid", PortGroups: []vsphere.DVPortGroup{{ID: "dvpg-1", Key: "dvpg-key", Name: "production", VLAN: "210"}}}
	data := assessment.ExportData{Run: assessment.Run{ID: 8, InventorySchemaVersion: "12"}, Contexts: []assessment.ContextRun{topologyContext("prod", "success", true)}, VMs: []assessment.ExportVM{{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-prod", VM: vsphere.VM{ID: "vm-1", Name: "app01", NICs: []vsphere.VMNIC{{Network: "wrong-display-name", NetworkID: "dvpg-key", SwitchID: "switch-uuid"}}}}}}, Resources: []assessment.ResourceObservation{topologyResource(t, "prod", "vc-prod", "dvswitch", dvs.ID, dvs.Name, dvs), topologyResource(t, "prod", "vc-prod", "network", "network-1", "wrong-display-name", vsphere.Network{Location: dvs.Location, ID: "network-1", Name: "wrong-display-name", VLAN: "210"})}}
	graph := Build(data)
	subject := graph.Resolve(KindVM, "app01", nil)[0]
	result := graph.Dependencies(subject, 1)
	var found bool
	for _, edge := range result.Edges {
		if edge.To.Kind == string(KindNetwork) && edge.Basis == BasisPortGroupKey && edge.Confidence == EdgeConfirmed {
			found = true
		}
	}
	if !found {
		t.Fatalf("dependencies=%+v", result)
	}
}

func TestPreV12NetworkReconstructionIsPartial(t *testing.T) {
	dvs := vsphere.DVSwitch{Location: vsphere.Location{Context: "prod"}, ID: "dvs-1", Name: "switch", UUID: "switch-uuid", PortGroups: []vsphere.DVPortGroup{{ID: "dvpg-1", Key: "dvpg-key", Name: "production", VLAN: "210"}}}
	data := assessment.ExportData{Run: assessment.Run{ID: 9, InventorySchemaVersion: "11"}, Contexts: []assessment.ContextRun{topologyContext("prod", "success", false)}, VMs: []assessment.ExportVM{{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-prod", VM: vsphere.VM{ID: "vm-1", Name: "app01", NICs: []vsphere.VMNIC{{Network: "production", NetworkID: "dvpg-key", SwitchID: "switch-uuid"}}}}}}, Resources: []assessment.ResourceObservation{topologyResource(t, "prod", "vc-prod", "dvswitch", dvs.ID, dvs.Name, dvs)}}
	graph := Build(data)
	subjects := graph.Resolve(KindNetwork, "production", nil)
	if len(subjects) != 1 {
		t.Fatalf("subjects=%+v", subjects)
	}
	result := graph.Topology(subjects[0])
	if result.Confidence != ConfidencePartial || len(result.Blind) != 1 || !strings.Contains(result.Blind[0].Reason, "predates network inventory") {
		t.Fatalf("result=%+v", result)
	}
}

func TestBuildAndQueriesAreDeterministic(t *testing.T) {
	data := datastoreSubjectData(t, "12", "ds-edge", false)
	left, right := Build(data), Build(data)
	if !reflect.DeepEqual(left, right) {
		t.Fatal("Build returned different graphs for identical input")
	}
	subject := left.Resolve(KindDatastore, "ds-prod", nil)[0]
	if !reflect.DeepEqual(left.BlastRadius(subject, 1), right.BlastRadius(subject, 1)) {
		t.Fatal("query output was not deterministic")
	}
}

// sameNameInOneVCenterData models what vcsim and many real estates produce: two
// datacenters in one vCenter, each with a local datastore and a standard
// network carrying the same display name. Local datastores report no backing
// identity, so name is the only thing the two observations share.
func sameNameInOneVCenterData(t *testing.T) assessment.ExportData {
	t.Helper()
	dc0DS := vsphere.Datastore{Location: vsphere.Location{Context: "prod", Datacenter: "DC0", Path: "/DC0/datastore/LocalDS_0"}, ID: "datastore-129", Name: "LocalDS_0", Backing: vsphere.DatastoreBacking{Local: true}}
	dc1DS := vsphere.Datastore{Location: vsphere.Location{Context: "prod", Datacenter: "DC1", Path: "/DC1/datastore/LocalDS_0"}, ID: "datastore-133", Name: "LocalDS_0", Backing: vsphere.DatastoreBacking{Local: true}}
	dc0Net := vsphere.Network{Location: vsphere.Location{Context: "prod", Datacenter: "DC0", Path: "/DC0/network/VM Network"}, ID: "network-6", Name: "VM Network"}
	dc1Net := vsphere.Network{Location: vsphere.Location{Context: "prod", Datacenter: "DC1", Path: "/DC1/network/VM Network"}, ID: "network-70", Name: "VM Network"}
	return assessment.ExportData{
		Run:      assessment.Run{ID: 11, InventorySchemaVersion: "12"},
		Contexts: []assessment.ContextRun{topologyContext("prod", "success", true)},
		VMs: []assessment.ExportVM{
			{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-prod", VM: vsphere.VM{ID: "vm-1", Name: "dc0-app", Datastores: []string{"LocalDS_0"}, Disks: []vsphere.VMDisk{{BackingPath: "[LocalDS_0] dc0-app/disk1.vmdk"}}}}},
			{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-prod", VM: vsphere.VM{ID: "vm-2", Name: "dc1-app", Datastores: []string{"LocalDS_0"}, Disks: []vsphere.VMDisk{{BackingPath: "[LocalDS_0] dc1-app/disk1.vmdk"}}}}},
		},
		Resources: []assessment.ResourceObservation{
			topologyResource(t, "prod", "vc-prod", "datastore", dc0DS.ID, dc0DS.Name, dc0DS),
			topologyResource(t, "prod", "vc-prod", "datastore", dc1DS.ID, dc1DS.Name, dc1DS),
			topologyResource(t, "prod", "vc-prod", "network", dc0Net.ID, dc0Net.Name, dc0Net),
			topologyResource(t, "prod", "vc-prod", "network", dc1Net.ID, dc1Net.Name, dc1Net),
		},
	}
}

func TestSameNamedObjectsInOneVCenterStayDistinct(t *testing.T) {
	graph := Build(sameNameInOneVCenterData(t))
	for _, tc := range []struct {
		kind Kind
		name string
		ids  []string
	}{
		{KindDatastore, "LocalDS_0", []string{"datastore-129", "datastore-133"}},
		{KindNetwork, "VM Network", []string{"network-6", "network-70"}},
	} {
		subjects := graph.Resolve(tc.kind, tc.name, nil)
		if len(subjects) != len(tc.ids) {
			t.Fatalf("%s %q: resolved %d subjects, want %d", tc.kind, tc.name, len(subjects), len(tc.ids))
		}
		seen := make(map[string]bool, len(subjects))
		for _, subject := range subjects {
			if len(subject.Members) != 1 {
				t.Fatalf("%s %q: subject has %d members, want the one object it was observed as", tc.kind, tc.name, len(subject.Members))
			}
			seen[subject.Members[0].ID] = true
		}
		for _, id := range tc.ids {
			if !seen[id] {
				t.Fatalf("%s %q: %s was absorbed into another subject; got %v", tc.kind, tc.name, id, seen)
			}
		}
	}
}

func TestSameNamedSubjectsKeepTheirOwnAncestry(t *testing.T) {
	graph := Build(sameNameInOneVCenterData(t))
	want := map[string]string{"network-6": "DC0", "network-70": "DC1"}
	for _, subject := range graph.Resolve(KindNetwork, "VM Network", nil) {
		id := subject.Members[0].ID
		result := graph.Topology(subject)
		var datacenters []string
		for _, edge := range result.Ancestors {
			if edge.From.Kind == "datacenter" {
				datacenters = append(datacenters, edge.From.Name)
			}
		}
		if len(datacenters) != 1 || datacenters[0] != want[id] {
			t.Fatalf("network %s ancestry names datacenters %v, want only %s", id, datacenters, want[id])
		}
	}
}

func TestAmbiguousNameIsUnresolvedRatherThanAttributed(t *testing.T) {
	graph := Build(sameNameInOneVCenterData(t))
	subject := graph.Resolve(KindVM, "dc0-app", nil)[0]
	result := graph.Dependencies(subject, 1)
	for _, edge := range result.Edges {
		if edge.To.Kind == string(KindDatastore) {
			t.Fatalf("a name shared by two datastores produced edge %+v, want it left unresolved", edge)
		}
	}
	// The disk path lower-cases the datastore name for joining and the VM's own
	// datastore list does not; one unresolved dependency must not be reported
	// twice, and it is reported the way vCenter spells it.
	if !reflect.DeepEqual(result.Unresolved, []string{"datastore LocalDS_0"}) {
		t.Fatalf("unresolved=%q, want one entry spelled as vCenter reports it", result.Unresolved)
	}
}

func TestBlastRadiusDoesNotSpreadAcrossSameNamedDatastores(t *testing.T) {
	graph := Build(sameNameInOneVCenterData(t))
	for _, subject := range graph.Resolve(KindDatastore, "LocalDS_0", nil) {
		if result := graph.BlastRadius(subject, 1); len(result.Edges) != 0 {
			t.Fatalf("datastore %s claims dependents %+v; name alone cannot tell the two apart", subject.Members[0].ID, result.Edges)
		}
	}
}
