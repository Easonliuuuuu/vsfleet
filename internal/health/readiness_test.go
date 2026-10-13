package health

import "testing"

func TestReadinessVerdicts(t *testing.T) {
	base := Report{SchemaVersion: ReportSchemaVersion, RunID: 7, Coverage: Coverage{Contexts: 1, CompleteContexts: 1}}
	for _, rule := range Rules() {
		if rule.Category == CategoryMigration {
			base.Rules = append(base.Rules, RuleStatus{Rule: rule.ID, Status: "evaluated", Result: "pass"})
		}
	}
	for _, tc := range []struct {
		name    string
		change  func(*Report)
		verdict string
	}{
		{"ready", func(*Report) {}, "ready"},
		{"blocked", func(r *Report) {
			r.Findings = []Finding{{Rule: "cdrom-connected", Category: CategoryMigration, Severity: SeverityWarning}}
		}, "blocked"},
		{"unknown result", func(r *Report) { r.Rules[0].Result = "unknown" }, "unknown"},
		{"skip result", func(r *Report) { r.Rules[0].Result = "skip" }, "unknown"},
		{"blind rule", func(r *Report) { r.Rules[0].Blind = []string{"vc-b"} }, "unknown"},
		{"partial rule", func(r *Report) { r.Rules[0].Partial = []PartialCoverage{{Context: "prod", Unevaluated: 1, Total: 2}} }, "unknown"},
		{"non-migration critical", func(r *Report) {
			r.Findings = []Finding{{Rule: "datastore-inaccessible", Category: CategoryAvailability, Severity: SeverityCritical}}
		}, "ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := base
			report.Rules = append([]RuleStatus(nil), base.Rules...)
			tc.change(&report)
			if got := Readiness(report); got.Verdict != tc.verdict {
				t.Fatalf("verdict=%q, want %q", got.Verdict, tc.verdict)
			}
		})
	}
}
