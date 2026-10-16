package report

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The "lab" row holds what the real RVTools 4.8.1.4 export of the nested lab
// writes for Cluster-DR: HA off, DRS on, and every HA setting still reported.
func TestWriteRVToolsVClusterSettings(t *testing.T) {
	yes := true
	threads, vmotions := int32(4), int32(0)
	lab := vsphere.Cluster{
		ID: "domain-c2005", Name: "Cluster-DR", Hosts: 1, ConfigStatus: "green", OverallStatus: "green",
		CPUThreads: &threads, VMotions: &vmotions, DRSEnabled: true,
		Settings: &vsphere.ClusterSettings{
			FailoverLevel: 1, AdmissionControl: true, HostMonitoring: "enabled", VMMonitoring: "vmMonitoringDisabled",
			HeartbeatDatastorePolicy: "allFeasibleDsWithUserPreference", IsolationResponse: "none", RestartPriority: "medium",
			VMTools:        &vsphere.ClusterVMToolsMonitoring{ClusterSettings: &yes, MaxFailures: 3, MaxFailureWindow: -1, FailureInterval: 30, MinUpTime: 120},
			DRSVMotionRate: 3, DPMBehavior: "automated", DPMHostPowerActionRate: 3,
		},
	}
	settingColumns := []string{"Config status", "NumCpuThreads", "Num VMotions", "Failover Level", "AdmissionControlEnabled", "Host monitoring",
		"HB Datastore Candidate Policy", "Isolation Response", "Restart Priority", "Cluster Settings", "Max Failures", "Max Failure Window",
		"Failure Interval", "Min Up Time", "VM Monitoring", "DRS vmotion rate", "DPM enabled", "DPM default behavior", "DPM Host Power Action Rate"}
	empty := map[string]string{}
	for _, header := range settingColumns {
		empty[header] = ""
	}
	for _, tc := range []struct {
		name    string
		cluster vsphere.Cluster
		want    map[string]string
	}{
		{"lab", lab, map[string]string{
			"Config status": "green", "OverallStatus": "green", "NumCpuThreads": "4", "Num VMotions": "0", "HA enabled": "FALSE",
			"Failover Level": "1", "AdmissionControlEnabled": "TRUE", "Host monitoring": "enabled",
			"HB Datastore Candidate Policy": "allFeasibleDsWithUserPreference", "Isolation Response": "none", "Restart Priority": "medium",
			"Cluster Settings": "TRUE", "Max Failures": "3", "Max Failure Window": "-1", "Failure Interval": "30", "Min Up Time": "120",
			"VM Monitoring": "vmMonitoringDisabled", "DRS enabled": "TRUE", "DRS vmotion rate": "3", "DPM enabled": "FALSE",
			"DPM default behavior": "automated", "DPM Host Power Action Rate": "3",
		}},
		{"captured before schema 25", vsphere.Cluster{ID: "domain-c9", Name: "Cluster-Lab", Hosts: 1}, empty},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := sampleExportData(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
			payload, err := json.Marshal(tc.cluster)
			if err != nil {
				t.Fatal(err)
			}
			data.Resources = append(data.Resources, assessment.ResourceObservation{Context: "prod", VCenterID: "vc-uuid", Kind: "cluster", ID: tc.cluster.ID, Name: tc.cluster.Name, Payload: payload})
			got := firstRowByHeader(t, data, "vCluster")
			for header, want := range tc.want {
				cell, found := got[header]
				if !found || cell != want {
					t.Errorf("%s = %q (column present: %v), want %q", header, cell, found, want)
				}
			}
		})
	}
}

// The vCluster columns vsfleet writes keep RVTools 4.8.1.4's relative order.
func TestVClusterHeadersFollowRVToolsOrder(t *testing.T) {
	rvtools := []string{"Name", "Config status", "OverallStatus", "NumHosts", "numEffectiveHosts", "TotalCpu", "NumCpuCores", "NumCpuThreads",
		"Effective Cpu", "TotalMemory", "Effective Memory", "Num VMotions", "HA enabled", "Failover Level", "AdmissionControlEnabled",
		"Host monitoring", "HB Datastore Candidate Policy", "Isolation Response", "Restart Priority", "Cluster Settings", "Max Failures",
		"Max Failure Window", "Failure Interval", "Min Up Time", "VM Monitoring", "DRS enabled", "DRS default VM behavior", "DRS vmotion rate",
		"DPM enabled", "DPM default behavior", "DPM Host Power Action Rate", "Object ID", "com.vmware.vcenter.cluster.edrs.upgradeHostAdded",
		"VI SDK Server", "VI SDK UUID"}
	position := map[string]int{}
	for i, h := range rvtools {
		position[h] = i
	}
	last := -1
	for _, h := range clusterHeaders {
		p, ok := position[h]
		if !ok {
			continue
		}
		if p < last {
			t.Errorf("%s is out of RVTools order", h)
		}
		last = p
	}
}

// firstRowByHeader exports data and returns the sheet's first data row keyed
// by header.
func firstRowByHeader(t *testing.T, data assessment.ExportData, sheet string) map[string]string {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteRVTools(&buf, data, healthReport(data)); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := f.GetRows(sheet)
	if err != nil || len(rows) < 2 {
		t.Fatalf("%s rows=%v err=%v", sheet, rows, err)
	}
	out := make(map[string]string, len(rows[0]))
	for i, h := range rows[0] {
		out[h] = ""
		if i < len(rows[1]) {
			out[h] = rows[1][i]
		}
	}
	return out
}
