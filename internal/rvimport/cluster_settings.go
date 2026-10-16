package rvimport

import "github.com/easonliuuuuu/vsfleet/internal/vsphere"

// clusterSettingsFromRow reads the vCluster HA, DRS and DPM columns. It
// returns nil when the row carries no Failover Level, as for a workbook that
// predates these columns or a standalone host vsfleet exported, so a missing
// setting is "not read" rather than an HA configuration of zeros. Every cell
// is read first so the column report sees each one.
func clusterSettingsFromRow(t sheetTable, row []string) *vsphere.ClusterSettings {
	failoverLevel := t.cell(row, "Failover Level")
	st := &vsphere.ClusterSettings{
		FailoverLevel:            int32(parseInt(failoverLevel)),
		AdmissionControl:         parseBool(t.cell(row, "AdmissionControlEnabled")),
		HostMonitoring:           t.cell(row, "Host monitoring"),
		HeartbeatDatastorePolicy: t.cell(row, "HB Datastore Candidate Policy"),
		IsolationResponse:        t.cell(row, "Isolation Response"),
		RestartPriority:          t.cell(row, "Restart Priority"),
		VMMonitoring:             t.cell(row, "VM Monitoring"),
		DRSVMotionRate:           int32(parseInt(t.cell(row, "DRS vmotion rate"))),
		DPMEnabled:               parseBool(t.cell(row, "DPM enabled")),
		DPMBehavior:              t.cell(row, "DPM default behavior"),
		DPMHostPowerActionRate:   int32(parseInt(t.cell(row, "DPM Host Power Action Rate"))),
	}
	clusterSettings := t.cell(row, "Cluster Settings")
	maxFailures := t.cell(row, "Max Failures")
	maxFailureWindow := t.cell(row, "Max Failure Window")
	failureInterval := t.cell(row, "Failure Interval")
	minUpTime := t.cell(row, "Min Up Time")
	if failoverLevel == "" {
		return nil
	}
	if clusterSettings != "" || maxFailures != "" || maxFailureWindow != "" || failureInterval != "" || minUpTime != "" {
		st.VMTools = &vsphere.ClusterVMToolsMonitoring{
			ClusterSettings:  parseOptBool(clusterSettings),
			MaxFailures:      int32(parseInt(maxFailures)),
			MaxFailureWindow: int32(parseInt(maxFailureWindow)),
			FailureInterval:  int32(parseInt(failureInterval)),
			MinUpTime:        int32(parseInt(minUpTime)),
		}
	}
	return st
}
