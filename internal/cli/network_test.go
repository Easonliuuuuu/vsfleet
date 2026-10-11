package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func newNetworkHistoryDB(t *testing.T) string {
	t.Helper()
	var results []assessment.ContextResult
	for _, item := range []struct {
		context, vcenter, clusterID, clusterName, hostID, hostName, dvsID, dvsName string
	}{
		{context: "prod", vcenter: "vc-prod", clusterID: "cluster-prod", clusterName: "cluster-prod", hostID: "host-prod", hostName: "esx-prod", dvsID: "dvs-prod", dvsName: "dvs-prod"},
		{context: "dr", vcenter: "vc-dr", clusterID: "cluster-dr", clusterName: "cluster-dr", hostID: "host-dr", hostName: "esx-dr", dvsID: "dvs-dr", dvsName: "dvs-dr"},
	} {
		cluster := vsphere.Cluster{Location: vsphere.Location{Context: item.context, Datacenter: "dc"}, ID: item.clusterID, Name: item.clusterName}
		host := vsphere.Host{Location: vsphere.Location{Context: item.context, Datacenter: "dc"}, ID: item.hostID, Name: item.hostName, Cluster: item.clusterName}
		switchValue := vsphere.DVSwitch{Location: vsphere.Location{Context: item.context, Datacenter: "dc"}, ID: item.dvsID, Name: item.dvsName, UUID: item.dvsID + "-uuid", MaxMTU: 1500, Hosts: []string{item.hostID}, PortGroups: []vsphere.DVPortGroup{{ID: item.dvsID + "-pg", Key: item.dvsID + "-pg-key", Name: "app", VLAN: "100"}}}
		success := func(kind string, resources ...assessment.ResourceObservation) assessment.CollectionResult {
			return assessment.CollectionResult{Kind: kind, Status: "success", Resources: resources}
		}
		results = append(results, assessment.ContextResult{Name: item.context, VCenterID: item.vcenter, Status: "success", Collections: []assessment.CollectionResult{
			success("vm"),
			success("host", resourcePayload(t, item.context, item.vcenter, "host", host.ID, host.Name, host)),
			success("cluster", resourcePayload(t, item.context, item.vcenter, "cluster", cluster.ID, cluster.Name, cluster)),
			success("resourcepool"), success("vapp"),
			success("dvswitch", resourcePayload(t, item.context, item.vcenter, "dvswitch", switchValue.ID, switchValue.Name, switchValue)),
			success("datastore"), success("network"), success("snapshot"),
		}})
	}
	return seedHistory(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), "12", results...)
}

func TestNetworkCompareCommandJSONShape(t *testing.T) {
	dbPath := newNetworkHistoryDB(t)
	stdout, stderr, err := runCLI(t, dbPath, "network", "compare", "cluster-prod", "cluster-dr", "-o", "json")
	if err != nil || stderr != "" {
		t.Fatalf("compare err=%v stderr=%q stdout=%s", err, stderr, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result["run_id"] != float64(1) || result["confidence"] != "complete" {
		t.Fatalf("unexpected JSON envelope: %s", stdout)
	}
	if matched, ok := result["matched"].([]any); !ok || len(matched) != 1 {
		t.Fatalf("expected one matched network: %s", stdout)
	}
}

func TestNetworkCommandsValidateArgumentsAndFlags(t *testing.T) {
	dbPath := newNetworkHistoryDB(t)
	_, _, err := runCLI(t, dbPath, "network", "compare", "only-one-cluster")
	if err == nil || !strings.Contains(err.Error(), "accepts between 2 and 3 arg") {
		t.Fatalf("compare argument validation error=%v", err)
	}
	_, _, err = runCLI(t, dbPath, "assessment", "network-readiness")
	if err == nil || !strings.Contains(err.Error(), "required flag") {
		t.Fatalf("readiness flag validation error=%v", err)
	}
}
