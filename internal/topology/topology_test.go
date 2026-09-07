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
