package health

import "testing"

func TestReadinessVerdicts(t *testing.T) {
	base := Report{SchemaVersion: ReportSchemaVersion, RunID: 7, Rules: []RuleStatus{
		{Rule: "bios-firmware", Status: "evaluated", Result: "pass"}, {Rule: "cdrom-connected", Status: "evaluated", Result: "pass"}, {Rule: "cluster-network-inconsistent", Status: "evaluated", Result: "pass"},
		{Rule: "custom-cpu-topology", Status: "evaluated", Result: "pass"}, {Rule: "custom-resource-allocation", Status: "evaluated", Result: "pass"}, {Rule: "extension-managed-vm", Status: "evaluated", Result: "pass"}, {Rule: "floppy-present", Status: "evaluated", Result: "pass"}, {Rule: "host-device-passthrough", Status: "evaluated", Result: "pass"}, {Rule: "manual-mac-address", Status: "evaluated", Result: "pass"}, {Rule: "rdm-present", Status: "evaluated", Result: "pass"}, {Rule: "secure-boot-enabled", Status: "evaluated", Result: "pass"}, {Rule: "shared-disk", Status: "evaluated", Result: "pass"}, {Rule: "usb-connected", Status: "evaluated", Result: "pass"}, {Rule: "vtpm-present", Status: "evaluated", Result: "pass"},
	}, Coverage: Coverage{Contexts: 1, CompleteContexts: 1}}
	for _, tc := range []struct {
		name    string
		report  Report
		verdict string
	}{
		{name: "ready", report: base, verdict: "ready"},
		{name: "blocked", report: Report{SchemaVersion: ReportSchemaVersion, RunID: 7, Findings: []Finding{{Rule: "cdrom-connected", Category: CategoryMigration, Severity: SeverityWarning}}}, verdict: "blocked"},
		{name: "blind is unknown", report: Report{SchemaVersion: ReportSchemaVersion, RunID: 7, Rules: []RuleStatus{{Rule: "cdrom-connected", Status: "evaluated", Result: "unknown", Blind: []string{"vc-b"}}}}, verdict: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Readiness(tc.report)
			if got.Verdict != tc.verdict {
				t.Fatalf("verdict=%q, want %q", got.Verdict, tc.verdict)
			}
		})
	}
}
