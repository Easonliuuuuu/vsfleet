package decommission

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func boolPtr(value bool) *bool { return &value }

func baseData(vm vsphere.VM) assessment.ExportData {
	collections := make([]assessment.CollectionRun, 0, 7)
	for _, kind := range []string{"vm", "host", "cluster", "resourcepool", "vapp", "dvswitch", "datastore", "network", "snapshot"} {
		collections = append(collections, assessment.CollectionRun{Kind: kind, Status: "empty"})
	}
	return assessment.ExportData{
		Run:      assessment.Run{ID: 42, InventorySchemaVersion: assessment.CurrentInventorySchemaVersion},
		Contexts: []assessment.ContextRun{{Name: "prod", VMStatus: "success", Collections: collections}},
		VMs:      []assessment.ExportVM{{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-prod", VM: vm}}},
	}
}

func findCheck(report SubjectReport, id string) Check {
	for _, check := range report.Checks {
		if check.ID == id {
			return check
		}
	}
	return Check{}
}

func TestEvaluateNoBlockersAndMetadataAdvisories(t *testing.T) {
	report := Evaluate(baseData(vsphere.VM{ID: "vm-1", Name: "app", PowerState: "poweredOff", ConnectionState: "connected", ConfigurationAvailable: true}), "app", nil)
	if report.Verdict != VerdictNoBlockers || len(report.Subjects) != 1 {
		t.Fatalf("report = %#v", report)
	}
	for _, id := range []string{"ownership", "backup-policy", "application-dependencies"} {
		if check := findCheck(report.Subjects[0], id); check.Status != StatusNotAssessed || check.Impact != ImpactAdvisory {
			t.Fatalf("%s check = %#v", id, check)
		}
	}
	if report.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %d", report.SchemaVersion)
	}
}

func TestEvaluateStrictBlockers(t *testing.T) {
	cases := []struct {
		name string
		vm   vsphere.VM
		id   string
	}{
		{name: "powered on", vm: vsphere.VM{PowerState: "poweredOn", ConnectionState: "connected", ConfigurationAvailable: true}, id: "power-state"},
		{name: "snapshot", vm: vsphere.VM{PowerState: "poweredOff", ConnectionState: "connected", ConfigurationAvailable: true, Snapshots: []vsphere.VMSnapshot{{ID: "snap-1", Name: "before-change"}}}, id: "snapshots"},
		{name: "cdrom", vm: vsphere.VM{PowerState: "poweredOff", ConnectionState: "connected", ConfigurationAvailable: true, CDROMs: []vsphere.VMCDROM{{Label: "installer", Connected: boolPtr(true)}}}, id: "mounted-media"},
		{name: "usb", vm: vsphere.VM{PowerState: "poweredOff", ConnectionState: "connected", ConfigurationAvailable: true, USBs: []vsphere.VMUSB{{Label: "token", Connected: boolPtr(true)}}}, id: "mounted-media"},
		{name: "inaccessible", vm: vsphere.VM{PowerState: "poweredOff", ConnectionState: "inaccessible", ConfigurationAvailable: true}, id: "connection-state"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.vm.ID = "vm-1"
			tc.vm.Name = "app"
			report := Evaluate(baseData(tc.vm), "app", nil)
			if report.Verdict != VerdictBlocked {
				t.Fatalf("verdict = %s, report = %#v", report.Verdict, report)
			}
			check := findCheck(report.Subjects[0], tc.id)
			if check.Status != StatusFail || check.Impact != ImpactBlocker {
				t.Fatalf("check = %#v", check)
			}
		})
	}
}

func TestEvaluateUnknownCoverageAndIdentity(t *testing.T) {
	vm := vsphere.VM{ID: "vm-1", Name: "app", ConnectionState: "connected", ConfigurationAvailable: true}
	report := Evaluate(baseData(vm), "app", nil)
	if report.Verdict != VerdictUnknown || findCheck(report.Subjects[0], "power-state").Status != StatusUnknown {
		t.Fatalf("missing power evidence report = %#v", report)
	}
	missing := Evaluate(baseData(vm), "does-not-exist", nil)
	if missing.Verdict != VerdictUnknown || findCheck(missing.Subjects[0], "identity").Status != StatusUnknown {
		t.Fatalf("missing identity report = %#v", missing)
	}
}

func TestEvaluateAmbiguityAndStrongCrossContextJoin(t *testing.T) {
	collections := make([]assessment.CollectionRun, 0, 7)
	for _, kind := range []string{"vm", "host", "cluster", "resourcepool", "vapp", "dvswitch", "datastore", "network", "snapshot"} {
		collections = append(collections, assessment.CollectionRun{Kind: kind, Status: "empty"})
	}
	data := assessment.ExportData{
		Run:      assessment.Run{ID: 7, InventorySchemaVersion: assessment.CurrentInventorySchemaVersion},
		Contexts: []assessment.ContextRun{{Name: "edge", VMStatus: "success", Collections: collections}, {Name: "prod", VMStatus: "success", Collections: collections}},
		VMs: []assessment.ExportVM{
			{Observation: assessment.Observation{Context: "edge", VCenterID: "vc-edge", VM: vsphere.VM{ID: "vm-edge", Name: "app", PowerState: "poweredOff", ConnectionState: "connected", ConfigurationAvailable: true}}},
			{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-prod", VM: vsphere.VM{ID: "vm-prod", Name: "app", PowerState: "poweredOff", ConnectionState: "connected", ConfigurationAvailable: true}}},
		},
	}
	ambiguous := Evaluate(data, "app", nil)
	if !ambiguous.Ambiguous || ambiguous.Verdict != VerdictUnknown || len(ambiguous.Subjects) != 2 {
		t.Fatalf("ambiguous report = %#v", ambiguous)
	}
	data.VMs[0].Observation.VM.InstanceUUID = "same-instance"
	data.VMs[1].Observation.VM.InstanceUUID = "same-instance"
	joined := Evaluate(data, "app", nil)
	if joined.Ambiguous || joined.Verdict != VerdictNoBlockers || len(joined.Subjects) != 1 || len(joined.Subjects[0].Subject.Members) != 2 {
		t.Fatalf("joined report = %#v", joined)
	}
	byUUID := Evaluate(data, "same-instance", nil)
	if byUUID.Verdict != VerdictNoBlockers || len(byUUID.Subjects) != 1 {
		t.Fatalf("UUID query report = %#v", byUUID)
	}
}

// Issue #527: a VM is judged on its own references. Each row starts from a VM
// that is fully resolved on a host with a LUN no datastore maps to.
func TestEvaluateJudgesTheVMsOwnReferences(t *testing.T) {
	const (
		datastore = "datastore-dependencies"
		network   = "network-relationships"
	)
	for _, tc := range []struct {
		name      string
		change    func(*vsphere.VM, *assessment.ExportData)
		verdict   string
		unknown   map[string]string
		wantLabel string
	}{
		{name: "an unmapped LUN on its host is not its gap", change: func(*vsphere.VM, *assessment.ExportData) {}, verdict: VerdictNoBlockers},
		{
			name: "a disk on a datastore not in the capture", verdict: VerdictUnknown, wantLabel: "missing-ds",
			change: func(vm *vsphere.VM, _ *assessment.ExportData) {
				vm.Disks = append(vm.Disks, vsphere.VMDisk{Label: "disk 2", BackingPath: "[missing-ds] app/app_1.vmdk"})
			},
			unknown: map[string]string{datastore: "datastore dependency relationships are incomplete: could not resolve missing-ds"},
		},
		{
			name: "a disk on a datastore name two datastores share", verdict: VerdictUnknown, wantLabel: "datastore twin",
			change: func(vm *vsphere.VM, data *assessment.ExportData) {
				vm.Disks = append(vm.Disks, vsphere.VMDisk{Label: "disk 2", BackingPath: "[twin] app/app_1.vmdk"})
				data.Resources = append(data.Resources,
					testResource(t, "datastore", "ds-2", "twin", vsphere.Datastore{ID: "ds-2", Name: "twin"}),
					testResource(t, "datastore", "ds-3", "twin", vsphere.Datastore{ID: "ds-3", Name: "twin"}))
			},
			unknown: map[string]string{datastore: "could not resolve datastore twin"},
		},
		{
			name: "a NIC on a network not in the capture", verdict: VerdictUnknown, wantLabel: "missing-net",
			change: func(vm *vsphere.VM, _ *assessment.ExportData) {
				vm.NICs = append(vm.NICs, vsphere.VMNIC{Label: "nic 2", Network: "missing-net"})
			},
			unknown: map[string]string{network: "network dependency relationships are incomplete: could not resolve missing-net"},
		},
		{
			name: "a raw device mapping sits on a LUN nothing tracks", verdict: VerdictUnknown,
			change: func(vm *vsphere.VM, _ *assessment.ExportData) {
				vm.Disks = append(vm.Disks, vsphere.VMDisk{Label: "disk 2", BackingPath: "[nfs-shared] app/app_rdm.vmdk", Raw: true})
			},
			unknown: map[string]string{datastore: `disk "disk 2" is a raw device mapping`},
		},
		{
			name: "a host not in the capture", verdict: VerdictUnknown, wantLabel: "esx-gone",
			change:  func(vm *vsphere.VM, _ *assessment.ExportData) { vm.Host = "esx-gone" },
			unknown: map[string]string{"placement": "could not resolve esx-gone"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := vsphere.VM{
				ID: "vm-1", Name: "app", PowerState: "poweredOff", ConnectionState: "connected", ConfigurationAvailable: true, Host: "esx-1",
				Disks: []vsphere.VMDisk{{Label: "disk 1", BackingPath: "[nfs-shared] app/app.vmdk"}},
				NICs:  []vsphere.VMNIC{{Label: "nic 1", Network: "dvpg-100", NetworkID: "dvportgroup-1", SwitchID: "50 aa"}},
			}
			data := baseData(vm)
			data.Resources = []assessment.ResourceObservation{
				testResource(t, "host", "host-1", "esx-1", vsphere.Host{ID: "host-1", Name: "esx-1", Multipaths: []vsphere.HostMultipath{{LUN: "Local ATA Disk"}}}),
				testResource(t, "datastore", "ds-1", "nfs-shared", vsphere.Datastore{ID: "ds-1", Name: "nfs-shared"}),
				testResource(t, "dvswitch", "dvs-1", "dvs", vsphere.DVSwitch{ID: "dvs-1", Name: "dvs", UUID: "50 aa", PortGroups: []vsphere.DVPortGroup{{ID: "dvportgroup-1", Key: "dvportgroup-1", Name: "dvpg-100"}}}),
			}
			tc.change(&vm, &data)
			data.VMs[0].Observation.VM = vm

			report := Evaluate(data, "app", nil)
			if report.Verdict != tc.verdict {
				t.Errorf("verdict = %s, want %s; checks = %+v", report.Verdict, tc.verdict, report.Subjects[0].Checks)
			}
			for _, id := range []string{datastore, network, "placement"} {
				check := findCheck(report.Subjects[0], id)
				want, unknown := tc.unknown[id]
				switch {
				case unknown && (check.Status != StatusUnknown || !strings.Contains(check.Message, want)):
					t.Errorf("%s = %+v, want unknown with %q", id, check, want)
				case !unknown && check.Status == StatusUnknown:
					t.Errorf("%s = %+v, want it unaffected", id, check)
				}
			}
			unresolved := strings.Join(report.Subjects[0].Unresolved, ",")
			if strings.Contains(unresolved, "Local ATA Disk") || (tc.wantLabel != "" && !strings.Contains(unresolved, tc.wantLabel)) {
				t.Errorf("unresolved = %q, want %q and not the host's LUN", unresolved, tc.wantLabel)
			}
		})
	}
}

func testResource(t *testing.T, kind, id, name string, value any) assessment.ResourceObservation {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return assessment.ResourceObservation{Context: "prod", VCenterID: "vc-prod", Kind: kind, ID: id, Name: name, Payload: payload}
}

func TestEvaluateUnresolvedDependenciesAreUnknown(t *testing.T) {
	connected := false
	data := baseData(vsphere.VM{
		ID: "vm-1", Name: "app", PowerState: "poweredOff", ConnectionState: "connected", ConfigurationAvailable: true,
		Disks: []vsphere.VMDisk{{Key: 1, Label: "disk", BackingPath: "[missing-ds] app/app.vmdk"}},
		NICs:  []vsphere.VMNIC{{Key: 2, Label: "nic", Network: "missing-network", Connected: &connected}},
	})
	report := Evaluate(data, "app", nil)
	if report.Verdict != VerdictUnknown {
		t.Fatalf("verdict = %s, report = %#v", report.Verdict, report)
	}
	if check := findCheck(report.Subjects[0], "datastore-dependencies"); check.Status != StatusUnknown {
		t.Fatalf("datastore dependency check = %#v", check)
	}
	if check := findCheck(report.Subjects[0], "network-relationships"); check.Status != StatusUnknown {
		t.Fatalf("network relationship check = %#v", check)
	}
}
