package rvimport

import (
	"reflect"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The "lab" cells are those of the real RVTools 4.8.1.4 export of the nested
// lab for Cluster-DR, where HA is off and its settings are still reported.
func TestVClusterSettingsAreReadBack(t *testing.T) {
	path := writeFixtureWorkbook(t, nil)
	lab := map[string]string{
		"Config status": "green", "OverallStatus": "green", "NumCpuThreads": "4", "Num VMotions": "0",
		"Failover Level": "1", "AdmissionControlEnabled": "True", "Host monitoring": "enabled",
		"HB Datastore Candidate Policy": "allFeasibleDsWithUserPreference", "Isolation Response": "none", "Restart Priority": "medium",
		"Cluster Settings": "True", "Max Failures": "3", "Max Failure Window": "-1", "Failure Interval": "30", "Min Up Time": "120",
		"VM Monitoring": "vmMonitoringDisabled", "DRS vmotion rate": "3", "DPM enabled": "False", "DPM default behavior": "automated",
		"DPM Host Power Action Rate": "3",
	}
	blank := map[string]string{}
	for header := range lab {
		blank[header] = ""
	}
	yes := true
	threads, vmotions := int32(4), int32(0)
	for _, tc := range []struct {
		name              string
		cells             map[string]string
		wantStatus        string
		threads, vmotions *int32
		want              *vsphere.ClusterSettings
	}{
		{"lab", lab, "green", &threads, &vmotions, &vsphere.ClusterSettings{
			FailoverLevel: 1, AdmissionControl: true, HostMonitoring: "enabled", VMMonitoring: "vmMonitoringDisabled",
			HeartbeatDatastorePolicy: "allFeasibleDsWithUserPreference", IsolationResponse: "none", RestartPriority: "medium",
			VMTools:        &vsphere.ClusterVMToolsMonitoring{ClusterSettings: &yes, MaxFailures: 3, MaxFailureWindow: -1, FailureInterval: 30, MinUpTime: 120},
			DRSVMotionRate: 3, DPMBehavior: "automated", DPMHostPowerActionRate: 3,
		}},
		{"not reported", blank, "", nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openFixture(t, path)
			for header, value := range tc.cells {
				setColumnValue(t, f, sheetVCluster, header, 2, value)
			}
			result, err := Parse(f, Options{})
			if err != nil {
				t.Fatal(err)
			}
			cl := findCluster(result, "alpha", "cluster-a")
			if cl == nil {
				t.Fatal("cluster-a not imported")
			}
			if cl.ConfigStatus != tc.wantStatus {
				t.Errorf("config status = %q, want %q", cl.ConfigStatus, tc.wantStatus)
			}
			if !reflect.DeepEqual(cl.CPUThreads, tc.threads) || !reflect.DeepEqual(cl.VMotions, tc.vmotions) {
				t.Errorf("threads, vMotions = %v, %v, want %v, %v", cl.CPUThreads, cl.VMotions, tc.threads, tc.vmotions)
			}
			if !reflect.DeepEqual(cl.Settings, tc.want) {
				t.Errorf("settings = %+v\nwant %+v", cl.Settings, tc.want)
			}
		})
	}
}

func findCluster(result *Result, contextName, name string) *vsphere.Cluster {
	for _, c := range result.contexts {
		if c.name != contextName {
			continue
		}
		for _, cl := range c.clusters {
			if cl.Name == name {
				return cl
			}
		}
	}
	return nil
}
