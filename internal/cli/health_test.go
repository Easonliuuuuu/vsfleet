package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func newHealthTestHistoryDB(t *testing.T, findings bool) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "history.db")
	s, err := assessment.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ctx := &config.Context{Name: "prod", Endpoint: "https://vc.example"}
	run, err := s.StartRunWithMetadata(context.Background(), "test", []*config.Context{ctx}, when, assessment.RunMetadata{InventorySchemaVersion: assessment.CurrentInventorySchemaVersion})
	if err != nil {
		t.Fatal(err)
	}
	vm := vsphere.VM{ID: "vm-1", Name: "app", PowerState: "poweredOn", ToolsState: "guestToolsNotRunning"}
	if !findings {
		vm.PowerState = "poweredOff"
		vm.ToolsState = "guestToolsRunning"
	} else {
		vm.ConnectionState = "orphaned"
	}
	if err := s.SaveContext(context.Background(), run.ID, assessment.ContextResult{
		Name: "prod", VCenterID: "vc-uuid", Status: "success",
		VMs: []assessment.Observation{{Context: "prod", VCenterID: "vc-uuid", VM: vm}},
	}, when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishRun(context.Background(), run.ID, when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return dbPath
}

func runHealth(t *testing.T, dbPath string, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	a := &App{HistoryPath: dbPath, Out: &out, Err: &errOut}
	root := NewRootCommand(a)
	root.SetArgs(append([]string{"--history-db", dbPath, "health"}, args...))
	defer func() { _ = a.Close(context.Background()) }()
	err := root.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func TestHealthCommandExitCodes(t *testing.T) {
	findingsDB := newHealthTestHistoryDB(t, true)
	stdout, _, err := runHealth(t, findingsDB, "latest", "--fail-on-findings")
	var coded interface{ ExitCode() int }
	if !errors.As(err, &coded) || coded.ExitCode() != 2 {
		t.Fatalf("health findings error=%v, code=%v, output=%s", err, coded, stdout)
	}
	if !strings.Contains(stdout, "tools-not-running") {
		t.Fatalf("health output=%s", stdout)
	}
	if !strings.Contains(stdout, "vm-orphaned") {
		t.Fatalf("health output omitted orphaned VM finding=%s", stdout)
	}

	cleanDB := newHealthTestHistoryDB(t, false)
	if _, _, err := runHealth(t, cleanDB, "latest", "--fail-on-findings"); err != nil {
		t.Fatalf("clean health returned %v", err)
	}
	if _, _, err := runHealth(t, cleanDB, "does-not-exist"); err == nil {
		t.Fatal("bad selector unexpectedly succeeded")
	}
}

func TestHealthCommandJSONAndRuleListing(t *testing.T) {
	db := newHealthTestHistoryDB(t, false)
	stdout, stderr, err := runHealth(t, db, "latest", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("invalid health JSON: %v\n%s", err, stdout)
	}
	if report["run_id"] != float64(1) {
		t.Fatalf("health JSON=%v", report)
	}
	if !strings.Contains(stderr, "datastore-zombie-vmdk") {
		t.Fatalf("health output did not explain skipped datastore browsing: %s", stderr)
	}

	stdout, _, err = runHealth(t, db, "--list-rules")
	if err != nil || !strings.Contains(stdout, "datastore-inaccessible") || !strings.Contains(stdout, "datastore-zombie-vmdk") || !strings.Contains(stdout, "vm-orphaned") || !strings.Contains(stdout, "tools-outdated") {
		t.Fatalf("rule listing err=%v output=%s", err, stdout)
	}
}

func TestHealthCommandReportsConnectedDevicesFromStoredEvidence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	s, err := assessment.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	run, err := s.StartRunWithMetadata(context.Background(), "test", []*config.Context{{Name: "prod", Endpoint: "https://vc.example", Datacenter: "dc-a"}}, when, assessment.RunMetadata{InventorySchemaVersion: assessment.CurrentInventorySchemaVersion})
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	connected := true
	vm := vsphere.VM{ID: "vm-1", Name: "app", CDROMs: []vsphere.VMCDROM{{Key: 301, Label: "CD/DVD drive 1", Connected: &connected, BackingType: "iso", BackingPath: "[ds] app/install.iso"}}, USBs: []vsphere.VMUSB{{Key: 401, Label: "USB device 1", Connected: &connected, BackingType: "remoteHost", BackingHost: "esx-1"}}}
	if err := s.SaveContext(context.Background(), run.ID, assessment.ContextResult{Name: "prod", VCenterID: "vc-uuid", Status: "success", VMs: []assessment.Observation{{Context: "prod", VCenterID: "vc-uuid", VM: vm}}, Collections: []assessment.CollectionResult{{Kind: "vm", Status: "success", ItemCount: 1}}}, when.Add(time.Minute)); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if _, err := s.FinishRun(context.Background(), run.ID, when.Add(time.Minute)); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runHealth(t, dbPath, "latest", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "datastore-zombie-vmdk") {
		t.Fatalf("health output did not explain skipped datastore browsing: %s", stderr)
	}
	if !strings.Contains(stdout, `"rule": "cdrom-connected"`) || !strings.Contains(stdout, `"rule": "usb-connected"`) || !strings.Contains(stdout, "install.iso") || !strings.Contains(stdout, "esx-1") {
		t.Fatalf("health JSON missing connected-device findings: %s", stdout)
	}
}
