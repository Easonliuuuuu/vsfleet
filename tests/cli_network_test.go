package tests

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/simulator"
)

func renameSimulatorCluster(t *testing.T, vc *vcenter, oldName, newName string) {
	t.Helper()
	client, err := govmomi.NewClient(context.Background(), mustURL(t, vc.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Login(context.Background(), url.UserPassword("user", "pass")); err != nil {
		t.Fatal(err)
	}
	finder := find.NewFinder(client.Client, false)
	cluster, err := finder.ClusterComputeResource(context.Background(), "/DC0/host/"+oldName)
	if err != nil {
		t.Fatal(err)
	}
	task, err := cluster.Rename(context.Background(), newName)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func mustURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/sdk"
	return parsed
}

func TestAssessmentNetworkComparisonUsesCrossVCenterEvidence(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	left := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 2
		m.Portgroup = 1
		m.Machine = 2
	})
	right := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 2
		m.Portgroup = 1
		m.Machine = 2
	})
	renameSimulatorCluster(t, left, "DC0_C0", "cluster-source")
	renameSimulatorCluster(t, right, "DC0_C0", "cluster-target")
	r := newRunner(t)
	r.addNonInteractiveContext("left", left, "env:VSFLEET_E2E_PASSWORD")
	r.addNonInteractiveContext("right", right, "env:VSFLEET_E2E_PASSWORD")
	addUnreachableContext(r, "offline")
	historyDB := filepath.Join(t.TempDir(), "history.db")
	if _, _, err := r.run("", "--history-db", historyDB, "assessment", "run", "--all-contexts"); err != nil {
		t.Fatalf("assessment run: %v", err)
	}

	stdout := r.mustRun("", "--history-db", historyDB, "network", "compare", "cluster-source", "cluster-target", "-o", "json")
	var result struct {
		Matched []struct {
			Source struct {
				Name string `json:"name"`
			} `json:"source"`
		} `json:"matched"`
		Blind []struct {
			Context string `json:"context"`
		} `json:"blind"`
		Confidence string `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid network comparison JSON: %v\n%s", err, stdout)
	}
	if len(result.Matched) == 0 {
		t.Fatalf("simulator networks were not compared:\n%s", stdout)
	}
	if result.Confidence != "unknown" {
		t.Fatalf("offline context should make confidence unknown: %s\n%s", result.Confidence, stdout)
	}
	foundOffline := false
	for _, blind := range result.Blind {
		if blind.Context == "offline" {
			foundOffline = true
		}
	}
	if !foundOffline {
		t.Fatalf("offline context was absent from blind coverage:\n%s", stdout)
	}

	stdout, _, err := r.run("", "--history-db", historyDB, "assessment", "network-readiness", "--source", "cluster-source", "--target", "cluster-target", "--fail-on-blockers")
	if err != nil {
		t.Fatalf("network readiness should not fail on an unknown result: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "Network readiness: UNKNOWN") {
		t.Fatalf("unexpected readiness output:\n%s", stdout)
	}
}
