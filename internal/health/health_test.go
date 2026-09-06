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

func TestRulesUseStableAlphabeticalOrder(t *testing.T) {
	want := []string{
		"cdrom-connected", "datastore-inaccessible", "datastore-space-low",
		"guest-disk-space-low", "host-disconnected", "host-in-maintenance",
		"snapshot-age", "tools-not-installed", "tools-not-running", "tools-outdated",
		"usb-connected",
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
