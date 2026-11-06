package report

// This file lists the RVTools 4.8 columns this profile leaves out on purpose,
// each with the reason. vsfleet collects a field when it uses the field in
// history or analysis, and exports it under the RVTools column it matches; a
// column that fails that rule is listed here instead. The list comes from
// comparing vsfleet's export with a real RVTools 4.8.2.1 export of the same
// vCenter; the comparison, not this package, holds RVTools' own column list.

// OmittedColumn is an RVTools column the profile does not write.
type OmittedColumn struct {
	Name   string `json:"column"`
	Reason string `json:"reason"`
}

// Reasons a column is left out.
const (
	omitUsage = "Live usage or runtime state: it changes on every capture, so storing it would add noise to every diff and to stored history. " +
		"Left out until such fields are stored apart from configuration."
	omitFT        = "Fault Tolerance is not collected yet."
	omitRowNumber = "RVTools' own row number, not vSphere data."
	omitSortKey   = "RVTools' own sort helper, not vSphere data."
)

// omittedColumns lists, per worksheet, the RVTools columns the worksheet
// leaves out, in RVTools' order.
var omittedColumns = map[string][]OmittedColumn{
	"vInfo": {
		{"Heartbeat", omitUsage}, {"Suspended To Memory", omitUsage}, {"Suspend time", omitUsage}, {"Overall Cpu Readiness", omitUsage},
		{"Active Memory", omitUsage}, {"FT State", omitFT}, {"FT Role", omitFT}, {"FT Latency", omitFT}, {"FT Bandwidth", omitFT},
		{"FT Sec. Latency", omitFT}, {"Vm Failover In Progress", omitUsage},
	},
	"vCPU": {{"Max", omitUsage}, {"Overall", omitUsage}, {"Entitlement", omitUsage}, {"DRS Entitlement", omitUsage}},
	"vMemory": {
		{"Overhead", omitUsage}, {"Max", omitUsage}, {"Consumed", omitUsage}, {"Consumed Overhead", omitUsage}, {"Private", omitUsage},
		{"Shared", omitUsage}, {"Swapped", omitUsage}, {"Ballooned", omitUsage}, {"Active", omitUsage}, {"Entitlement", omitUsage},
		{"DRS Entitlement", omitUsage},
	},
	"vDisk":      {{"Internal Sort Column", omitSortKey}},
	"vPartition": {{"Internal Sort Column", omitSortKey}},
	"vNetwork":   {{"Internal Sort Column", omitSortKey}},
	"vCD":        {{"VMRef", omitRowNumber}},
	"vUSB":       {{"VMRef", omitRowNumber}},
	"vTools": {
		{"Upgradeable", "Tools Version Status already says whether an upgrade is available, and RVTools' rule for this column could not be checked: every lab VM shows No."},
		{"App status", omitUsage}, {"Heartbeat status", omitUsage}, {"VMRef", omitRowNumber},
	},
	"vRP": usageColumns("CPU maxUsage", "CPU overallUsage", "CPU reservationUsed", "CPU reservationUsedForVm", "CPU unreservedForPool",
		"CPU unreservedForVm", "Mem maxUsage", "Mem overallUsage", "Mem reservationUsed", "Mem reservationUsedForVm", "Mem unreservedForPool",
		"Mem unreservedForVm", "QS overallCpuDemand", "QS overallCpuUsage", "QS staticCpuEntitlement", "QS distributedCpuEntitlement",
		"QS balloonedMemory", "QS compressedMemory", "QS consumedOverheadMemory", "QS distributedMemoryEntitlement", "QS guestMemoryUsage",
		"QS hostMemoryUsage", "QS overheadMemory", "QS privateMemory", "QS sharedMemory", "QS staticMemoryEntitlement", "QS swappedMemory"),
	"vCluster": {{"com.vmware.vcenter.cluster.edrs.upgradeHostAdded", "A vCenter-internal cluster option RVTools lists as a column, not a setting an operator configures."}},
	"vHost": {
		{"Compliance Check State", "Host profile compliance is not collected."},
		{"vSAN Fault Domain Name", "vSAN is not collected."},
		{"Console", "ESX service console memory (config.consoleReservation). ESXi has no service console, so the value is always empty and RVTools writes 0."},
		{"VM Used memory", omitUsage}, {"VM Memory Swapped", omitUsage}, {"VM Memory Ballooned", omitUsage},
		{"Assigned License(s)", "Needs the Global.Licenses privilege, which a read-only account usually lacks. A run captured with --include-licenses lists host license assignments on vsfleetLicenseAssignment."},
		{"Certificate Status", "Not collected; vHost carries the certificate's issuer, subject and dates instead."},
	},
}

func usageColumns(names ...string) []OmittedColumn {
	out := make([]OmittedColumn, len(names))
	for i, n := range names {
		out[i] = OmittedColumn{n, omitUsage}
	}
	return out
}

// OmittedSheets describes the RVTools worksheets this profile does not write
// at all. Each has no columns of its own: its RVTools columns are all in
// Omitted.
func OmittedSheets() []SheetSpec {
	return []SheetSpec{{
		Name:       "vMetaData",
		Compatible: true,
		NotWritten: true,
		Note: "Not written. It holds RVTools' own version and the time the workbook was created. vsfleet records where and when a capture ran on " +
			"vSource and vsfleetCoverage instead. An RVTools workbook's vMetaData is still read on import, for the capture time.",
		Columns: []ColumnSpec{},
		Omitted: []OmittedColumn{
			{"RVTools major version", "RVTools' own version."}, {"RVTools version", "RVTools' own version."},
			{"xlsx creation datetime", "When RVTools wrote the workbook; vsfleetCoverage records when the capture ran."},
			{"Server", "The vCenter RVTools read; every worksheet carries it as VI SDK Server."},
		},
	}}
}
