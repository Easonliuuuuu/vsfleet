package tests

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"

	"github.com/easonliuuuuu/vsfleet/internal/health"
)

// TestAssessmentReportsDeletedPortGroupAsDanglingReference reproduces issue
// #403: a VM NIC still backed by a deleted distributed port group used to mark
// the network collection partial as possible permission-hidden inventory. The
// port group is confirmed deleted, so the collection stays complete and the
// stale backing is an informational finding instead.
func TestAssessmentReportsDeletedPortGroupAsDanglingReference(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 1
		m.Portgroup = 1
		m.Machine = 1
	})
	portgroup := deletePortGroup(t, vc, "DC0_DVPG0")

	r := newRunner(t)
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")
	historyDB := filepath.Join(t.TempDir(), "history.db")
	if _, _, err := r.run("", "--history-db", historyDB, "assessment", "run", "--context", "lab", "--fail-on-partial"); err != nil {
		t.Fatalf("assessment run with a dangling NIC backing: %v", err)
	}

	reportJSON := r.mustRun("", "--history-db", historyDB, "-o", "json", "assessment", "report", "latest")
	var report struct {
		Coverage []struct {
			Kind   string `json:"kind"`
			Status string `json:"status"`
			Error  string `json:"error"`
		} `json:"coverage"`
	}
	if err := json.Unmarshal([]byte(reportJSON), &report); err != nil {
		t.Fatal(err)
	}
	for _, row := range report.Coverage {
		if row.Kind == "network" && (row.Status != "success" || row.Error != "") {
			t.Fatalf("network coverage status=%q error=%q, want success: a deleted port group is not hidden inventory", row.Status, row.Error)
		}
	}

	findingsJSON := r.mustRun("", "--history-db", historyDB, "-o", "json", "assessment", "findings", "latest")
	var findings health.Report
	if err := json.Unmarshal([]byte(findingsJSON), &findings); err != nil {
		t.Fatal(err)
	}
	var dangling []string
	for _, f := range findings.Findings {
		if f.Rule != "vm-network-missing" {
			continue
		}
		if f.Severity != health.SeverityInfo || !strings.Contains(f.Message, portgroup) {
			t.Errorf("finding %+v, want an info finding naming %s", f, portgroup)
		}
		dangling = append(dangling, f.Object.Name)
	}
	if want := []string{"DC0_C0_RP0_VM0", "DC0_H0_VM0"}; !slices.Equal(dangling, want) {
		t.Fatalf("vm-network-missing findings for %v, want %v: %s", dangling, want, findingsJSON)
	}
}

// deletePortGroup deletes distributed port group name. vcsim backs every VM
// NIC with the first distributed port group, so each NIC is left with a stale
// backing, as a real vCenter leaves one. It returns the port group's
// managed-object id.
func deletePortGroup(t *testing.T, vc *vcenter, name string) string {
	t.Helper()
	ctx := context.Background()
	client, err := govmomi.NewClient(ctx, mustURL(t, vc.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Login(ctx, url.UserPassword("user", "pass")); err != nil {
		t.Fatal(err)
	}
	finder := find.NewFinder(client.Client, true)
	dc, err := finder.DefaultDatacenter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	finder.SetDatacenter(dc)
	network, err := finder.Network(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	pg, ok := network.(*object.DistributedVirtualPortgroup)
	if !ok {
		t.Fatalf("%s is a %T, want a distributed port group", name, network)
	}
	task, err := pg.Destroy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	return pg.Reference().Value
}
