package health

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func healthFixture(schema string, finish time.Time) assessment.ExportData {
	hostDisconnected, _ := json.Marshal(vsphere.Host{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "host-2", Name: "esx-2", ConnectionState: "disconnected"})
	hostMaintenance, _ := json.Marshal(vsphere.Host{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "host-1", Name: "esx-1", ConnectionState: "connected", InMaintenance: true})
	dsLow, _ := json.Marshal(vsphere.Datastore{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "ds-1", Name: "slow", Accessible: true, CapacityBytes: 100, FreeBytes: 9})
	dsBad, _ := json.Marshal(vsphere.Datastore{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "ds-2", Name: "broken", Accessible: false, CapacityBytes: 100, FreeBytes: 50})
	created := finish.Add(-31 * 24 * time.Hour)
	return assessment.ExportData{
		Run:      assessment.Run{ID: 42, InventorySchemaVersion: schema, FinishedAt: finish},
		Contexts: []assessment.ContextRun{{Name: "prod", Datacenter: "dc-a", FinishedAt: finish}},
		VMs: []assessment.ExportVM{
			{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-1", VM: vsphere.VM{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "vm-2", Name: "not-installed", PowerState: "poweredOn", ToolsState: "guestToolsNotRunning", ToolsVersionStatus: "guestToolsNotInstalled", Partitions: []vsphere.VMPartition{{Path: "/", CapacityBytes: 100, FreeBytes: 9}}}}, Snapshots: []vsphere.VMSnapshot{{ID: "snap-2", Name: "pre-patch", CreateTime: created}}},
			{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-1", VM: vsphere.VM{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "vm-1", Name: "outdated", PowerState: "poweredOn", ToolsState: "guestToolsRunning", ToolsVersionStatus: "guestToolsNeedUpgrade"}}},
		},
		Resources: []assessment.ResourceObservation{
			{Context: "prod", VCenterID: "vc-1", Kind: "host", ID: "host-1", Name: "esx-1", Payload: hostMaintenance},
			{Context: "prod", VCenterID: "vc-1", Kind: "datastore", ID: "ds-2", Name: "broken", Payload: dsBad},
			{Context: "prod", VCenterID: "vc-1", Kind: "host", ID: "host-2", Name: "esx-2", Payload: hostDisconnected},
			{Context: "prod", VCenterID: "vc-1", Kind: "datastore", ID: "ds-1", Name: "slow", Payload: dsLow},
		},
	}
}

func TestEvaluateInitialRules(t *testing.T) {
	data := healthFixture("5", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	report := Evaluate(data, Options{Thresholds: Thresholds{SnapshotAge: 30 * 24 * time.Hour, DatastoreFreePct: 10, GuestDiskFreePct: 10}})
	got := make(map[string]int)
	for _, finding := range report.Findings {
		got[finding.Rule]++
	}
	want := map[string]int{
		"datastore-inaccessible": 1,
		"datastore-space-low":    1,
		"guest-disk-space-low":   1,
		"host-disconnected":      1,
		"host-in-maintenance":    1,
		"snapshot-age":           1,
		"tools-not-installed":    1,
		"tools-not-running":      1,
		"tools-outdated":         1,
	}
	for id, count := range want {
		if got[id] != count {
			t.Errorf("%s findings=%d, want %d", id, got[id], count)
		}
	}
	if report.Counts.Total != 9 || report.Counts.Info != 1 || report.Counts.Warning != 6 || report.Counts.Critical != 2 {
		t.Fatalf("counts=%+v", report.Counts)
	}
}

func TestEvaluateMarksFailedCollectionUnknown(t *testing.T) {
	data := assessment.ExportData{
		Run:      assessment.Run{ID: 50, InventorySchemaVersion: assessment.CurrentInventorySchemaVersion},
		Contexts: []assessment.ContextRun{{Name: "unreachable", VMStatus: "success", Collections: []assessment.CollectionRun{{Kind: "vm", Status: "success"}, {Kind: "datastore", Status: "failed", Error: "permission denied"}}}},
	}
	report := Evaluate(data, Options{Thresholds: DefaultThresholds()})
	for _, status := range report.Rules {
		if status.Rule == "datastore-space-low" {
			if status.Result != "unknown" || len(status.Blind) != 1 || status.Blind[0] != "unreachable" {
				t.Fatalf("datastore coverage status=%+v", status)
			}
			return
		}
	}
	t.Fatal("datastore-space-low status was not reported")
}

func TestEvaluateMigrationReadinessRules(t *testing.T) {
	trueValue := true
	hostOne, _ := json.Marshal(vsphere.Host{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "host-1", Name: "esx-1", Cluster: "cluster-a", VSwitches: []vsphere.HostVSwitch{{Name: "vSwitch0", MTU: 1500, Uplinks: []string{"vmnic0"}}}, Multipaths: []vsphere.HostMultipath{{LUN: "naa.1", PathCount: 1, Active: 1}}})
	hostTwo, _ := json.Marshal(vsphere.Host{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "host-2", Name: "esx-2", Cluster: "cluster-a", VSwitches: []vsphere.HostVSwitch{{Name: "vSwitch0", MTU: 9000, Uplinks: []string{"vmnic0", "vmnic1"}}}, PortGroups: []vsphere.HostPortGroup{{Name: "migration", Switch: "vSwitch0", Promiscuous: &trueValue}}})
	dvs, _ := json.Marshal(vsphere.DVSwitch{Location: vsphere.Location{Datacenter: "dc-a"}, ID: "dvs-1", Name: "dvSwitch0", Hosts: []string{"esx-1"}, PortGroups: []vsphere.DVPortGroup{{Name: "migration", Promiscuous: &trueValue}}})
	data := assessment.ExportData{Run: assessment.Run{ID: 51, InventorySchemaVersion: "10"}, Contexts: []assessment.ContextRun{{Name: "prod", VMStatus: "empty", Collections: []assessment.CollectionRun{{Kind: "vm", Status: "empty"}, {Kind: "host", Status: "success"}, {Kind: "dvswitch", Status: "success"}}}}, Resources: []assessment.ResourceObservation{{Context: "prod", VCenterID: "vc-1", Kind: "host", ID: "host-1", Name: "esx-1", Payload: hostOne}, {Context: "prod", VCenterID: "vc-1", Kind: "host", ID: "host-2", Name: "esx-2", Payload: hostTwo}, {Context: "prod", VCenterID: "vc-1", Kind: "dvswitch", ID: "dvs-1", Name: "dvSwitch0", Payload: dvs}}}
	report := Evaluate(data, Options{})
	want := map[string]bool{"cluster-network-inconsistent": false, "dvswitch-host-coverage": false, "host-path-redundancy": false, "portgroup-promiscuous": false, "dvportgroup-promiscuous": false}
	for _, finding := range report.Findings {
		if _, ok := want[finding.Rule]; ok {
			want[finding.Rule] = true
			if finding.Recommendation == "" || len(finding.Evidence) == 0 {
				t.Errorf("%s missing recommendation/evidence: %+v", finding.Rule, finding)
			}
		}
	}
	for rule, found := range want {
		if !found {
			t.Errorf("%s did not fire", rule)
		}
	}
}

func TestRulesUseStableAlphabeticalOrder(t *testing.T) {
	want := []string{
		"cdrom-connected", "cluster-network-inconsistent", "datastore-inaccessible", "datastore-space-low", "datastore-zombie-vmdk",
		"dvportgroup-promiscuous", "dvswitch-host-coverage", "guest-disk-space-low", "host-disconnected", "host-in-maintenance", "host-path-redundancy", "portgroup-promiscuous",
		"snapshot-age", "tools-not-installed", "tools-not-running", "tools-outdated", "usb-connected", "vm-inaccessible", "vm-orphaned",
	}
	rules := Rules()
	if len(rules) != len(want) {
		t.Fatalf("rule count=%d, want %d", len(rules), len(want))
	}
	for i, rule := range rules {
		if rule.ID != want[i] {
			t.Errorf("rule %d=%q, want %q", i, rule.ID, want[i])
		}
	}
}

func TestEvaluateVMConnectionRules(t *testing.T) {
	data := assessment.ExportData{
		Run:      assessment.Run{ID: 45, InventorySchemaVersion: "7"},
		Contexts: []assessment.ContextRun{{Name: "prod", Datacenter: "dc-a"}},
		VMs: []assessment.ExportVM{
			{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-1", VM: vsphere.VM{ID: "vm-orphan", Name: "orphan", ConnectionState: "orphaned"}}},
			{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-1", VM: vsphere.VM{ID: "vm-broken", Name: "broken", ConnectionState: "notResponding"}}},
			{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-1", VM: vsphere.VM{ID: "vm-ok", Name: "ok", ConnectionState: "connected"}}},
			{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-1", VM: vsphere.VM{ID: "tpl", Name: "template", IsTemplate: true, ConnectionState: "orphaned"}}},
		},
	}
	report := Evaluate(data, Options{})
	got := map[string]Finding{}
	for _, finding := range report.Findings {
		if finding.Rule == "vm-inaccessible" || finding.Rule == "vm-orphaned" {
			got[finding.Rule] = finding
		}
	}
	if len(got) != 2 || got["vm-orphaned"].Object.ID != "vm-orphan" || !strings.Contains(got["vm-inaccessible"].Message, "notResponding") {
		t.Fatalf("VM connection findings=%+v", got)
	}
	for _, status := range report.Rules {
		if (status.Rule == "vm-inaccessible" || status.Rule == "vm-orphaned") && status.Status != "evaluated" {
			t.Errorf("%s status=%q, want evaluated", status.Rule, status.Status)
		}
	}
}

func TestEvaluateZombieVMDKConservativelyMatchesReferencesAndDeltas(t *testing.T) {
	datastorePayload, _ := json.Marshal(vsphere.Datastore{
		Location:     vsphere.Location{Datacenter: "dc-a"},
		ID:           "ds-1",
		Name:         "datastore-1",
		BrowseStatus: "success",
		Files: []vsphere.DatastoreFile{
			{Path: "[datastore-1] app/disk.vmdk", SizeBytes: 8 << 30},
			{Path: "[datastore-1] app/disk-000001.vmdk", SizeBytes: 2 << 30},
			{Path: "[datastore-1] orphan/orphan.vmdk", SizeBytes: 4 << 30},
			{Path: "[datastore-1] templates/golden.vmdk", SizeBytes: 1 << 30},
		},
	})
	data := assessment.ExportData{
		Run: assessment.Run{ID: 46, InventorySchemaVersion: "7"},
		VMs: []assessment.ExportVM{
			{Observation: assessment.Observation{VM: vsphere.VM{Disks: []vsphere.VMDisk{{BackingPath: "[DATASTORE-1] app/disk.vmdk"}}}}},
			{Observation: assessment.Observation{VM: vsphere.VM{IsTemplate: true, Disks: []vsphere.VMDisk{{BackingPath: "[datastore-1] templates/golden.vmdk"}}}}},
		},
		Resources: []assessment.ResourceObservation{{Context: "prod", VCenterID: "vc-1", Kind: "datastore", ID: "ds-1", Name: "datastore-1", Payload: datastorePayload}},
	}
	report := Evaluate(data, Options{})
	var zombies []Finding
	for _, finding := range report.Findings {
		if finding.Rule == "datastore-zombie-vmdk" {
			zombies = append(zombies, finding)
		}
	}
	if len(zombies) != 1 || !strings.Contains(zombies[0].Message, "orphan/orphan.vmdk") {
		t.Fatalf("zombie findings=%+v", zombies)
	}

	data.Resources[0].Payload, _ = json.Marshal(vsphere.Datastore{ID: "ds-1", Name: "datastore-1"})
	report = Evaluate(data, Options{})
	for _, status := range report.Rules {
		if status.Rule == "datastore-zombie-vmdk" {
			if status.Status != "not-evaluated" || status.Reason != "a capture run with --browse-datastores" {
				t.Fatalf("zombie status=%+v", status)
			}
			return
		}
	}
	t.Fatal("datastore-zombie-vmdk status was not reported")
}

func TestEvaluateThresholdBoundaries(t *testing.T) {
	finish := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	data := healthFixture("5", finish)
	data.VMs[0].Observation.VM.Partitions[0].FreeBytes = 10
	data.VMs[0].Snapshots[0].CreateTime = finish.Add(-30 * 24 * time.Hour)
	data.Resources[1].Payload, _ = json.Marshal(vsphere.Datastore{ID: "ds-2", Name: "broken", Accessible: false, CapacityBytes: 100, FreeBytes: 10})
	data.Resources[3].Payload, _ = json.Marshal(vsphere.Datastore{ID: "ds-1", Name: "slow", Accessible: true, CapacityBytes: 100, FreeBytes: 10})
	report := Evaluate(data, Options{Thresholds: Thresholds{SnapshotAge: 30 * 24 * time.Hour, DatastoreFreePct: 10, GuestDiskFreePct: 10}})
	for _, finding := range report.Findings {
		if finding.Rule == "guest-disk-space-low" || finding.Rule == "datastore-space-low" {
			t.Fatalf("free-space rule fired at threshold: %+v", finding)
		}
	}
	foundSnapshot := false
	for _, finding := range report.Findings {
		foundSnapshot = foundSnapshot || finding.Rule == "snapshot-age"
	}
	if !foundSnapshot {
		t.Fatal("snapshot-age did not fire at threshold")
	}
}

func TestEvaluateIsDeterministicAndRunRelative(t *testing.T) {
	finish := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	data := healthFixture("5", finish)
	first := Evaluate(data, Options{Thresholds: DefaultThresholds()})
	second := Evaluate(data, Options{Thresholds: DefaultThresholds()})
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated evaluation differs:\nfirst=%+v\nsecond=%+v", first, second)
	}
	if first.Findings[0].Object.Context != "prod" {
		t.Fatalf("finding context=%q", first.Findings[0].Object.Context)
	}
	data.Run.FinishedAt = finish.Add(365 * 24 * time.Hour)
	third := Evaluate(data, Options{Thresholds: DefaultThresholds()})
	if !reflect.DeepEqual(first, third) {
		t.Fatal("evaluation changed when only the run finish time changed; context finish should be authoritative")
	}
}

func TestEvaluateSchemaGatesVersionAndPartitionRules(t *testing.T) {
	data := healthFixture("2", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	report := Evaluate(data, Options{Thresholds: DefaultThresholds()})
	statuses := make(map[string]string)
	for _, status := range report.Rules {
		statuses[status.Rule] = status.Status
	}
	for _, id := range []string{"cdrom-connected", "usb-connected", "guest-disk-space-low", "tools-not-installed", "tools-outdated"} {
		if statuses[id] != "not-evaluated" {
			t.Errorf("%s status=%q, want not-evaluated", id, statuses[id])
		}
	}
}

func TestEvaluateConnectedDeviceRulesOnlyReportConnectedDevices(t *testing.T) {
	cdConnected, cdDisconnected := true, false
	usbConnected, usbDisconnected := true, false
	data := assessment.ExportData{
		Run:      assessment.Run{ID: 43, InventorySchemaVersion: "6"},
		Contexts: []assessment.ContextRun{{Name: "prod", Datacenter: "dc-a"}},
		VMs: []assessment.ExportVM{{Observation: assessment.Observation{Context: "prod", VCenterID: "vc-1", VM: vsphere.VM{
			Location: vsphere.Location{Datacenter: "dc-a"}, ID: "vm-1", Name: "app",
			CDROMs: []vsphere.VMCDROM{
				{Key: 102, Label: "CD/DVD drive 2", Connected: &cdDisconnected},
				{Key: 101, Label: "CD/DVD drive 1", Connected: &cdConnected, BackingType: "iso", BackingPath: "[ds] app/install.iso"},
			},
			USBs: []vsphere.VMUSB{
				{Key: 202, Label: "USB device 2", Connected: &usbDisconnected},
				{Key: 201, Label: "USB device 1", Connected: &usbConnected, BackingType: "remoteHost", BackingHost: "esx-1"},
			},
		}}}},
	}
	report := Evaluate(data, Options{})
	var findings []Finding
	for _, finding := range report.Findings {
		if finding.Rule == "cdrom-connected" || finding.Rule == "usb-connected" {
			findings = append(findings, finding)
		}
	}
	if len(findings) != 2 {
		t.Fatalf("connected-device findings = %d, want 2: %+v", len(findings), findings)
	}
	if findings[0].Rule != "cdrom-connected" || !strings.Contains(findings[0].Message, "install.iso") {
		t.Errorf("CD-ROM finding = %+v", findings[0])
	}
	if findings[1].Rule != "usb-connected" || !strings.Contains(findings[1].Message, "esx-1") {
		t.Errorf("USB finding = %+v", findings[1])
	}
	for _, finding := range findings {
		if finding.Severity != SeverityWarning || finding.Object.ID != "vm-1" {
			t.Errorf("finding metadata = %+v", finding)
		}
	}
	first := Evaluate(data, Options{})
	data.VMs[0].Observation.VM.CDROMs[0], data.VMs[0].Observation.VM.CDROMs[1] = data.VMs[0].Observation.VM.CDROMs[1], data.VMs[0].Observation.VM.CDROMs[0]
	data.VMs[0].Observation.VM.USBs[0], data.VMs[0].Observation.VM.USBs[1] = data.VMs[0].Observation.VM.USBs[1], data.VMs[0].Observation.VM.USBs[0]
	second := Evaluate(data, Options{})
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("connected-device evaluation is not deterministic:\nfirst=%+v\nsecond=%+v", first, second)
	}
}

func TestEvaluateConnectedDeviceRulesAreSchemaGated(t *testing.T) {
	connected := true
	data := assessment.ExportData{
		Run: assessment.Run{ID: 44, InventorySchemaVersion: "5"},
		VMs: []assessment.ExportVM{{Observation: assessment.Observation{VM: vsphere.VM{
			ID: "vm-1", Name: "app", CDROMs: []vsphere.VMCDROM{{Connected: &connected}}, USBs: []vsphere.VMUSB{{Connected: &connected}},
		}}}},
	}
	report := Evaluate(data, Options{})
	for _, status := range report.Rules {
		if (status.Rule == "cdrom-connected" || status.Rule == "usb-connected") && status.Status != "not-evaluated" {
			t.Errorf("%s status=%q, want not-evaluated", status.Rule, status.Status)
		}
	}
	for _, finding := range report.Findings {
		if finding.Rule == "cdrom-connected" || finding.Rule == "usb-connected" {
			t.Fatalf("schema-gated rule emitted finding: %+v", finding)
		}
	}
}
