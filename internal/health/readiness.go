package health

// ReadinessReport is the migration-focused verdict over an evaluated health
// report. It deliberately does not access the assessment store or recount the
// estate.
type ReadinessReport struct {
	SchemaVersion int          `json:"schema_version"`
	RunID         int64        `json:"run_id"`
	Verdict       string       `json:"verdict"`
	Blockers      []Finding    `json:"blockers"`
	Advisories    []Finding    `json:"advisories"`
	Unresolved    []RuleStatus `json:"unresolved"`
	Coverage      Coverage     `json:"coverage"`
}

// Readiness derives the migration verdict from a health report. A finding at
// warning or critical severity in the migration category blocks. Information
// findings remain actionable advisories. Missing evidence can never produce a
// ready verdict.
func Readiness(report Report) ReadinessReport {
	out := ReadinessReport{
		SchemaVersion: report.SchemaVersion,
		RunID:         report.RunID,
		Verdict:       "ready",
		Blockers:      make([]Finding, 0),
		Advisories:    make([]Finding, 0),
		Unresolved:    make([]RuleStatus, 0),
		Coverage:      report.Coverage,
	}
	rulesByID := make(map[string]Rule, len(Rules()))
	for _, rule := range Rules() {
		rulesByID[rule.ID] = rule
	}
	for _, finding := range report.Findings {
		if finding.Category != CategoryMigration {
			continue
		}
		if severityRank(finding.Severity) >= severityRank(SeverityWarning) {
			out.Blockers = append(out.Blockers, finding)
		} else {
			out.Advisories = append(out.Advisories, finding)
		}
	}
	seen := make(map[string]bool, len(report.Rules))
	for _, status := range report.Rules {
		seen[status.Rule] = true
		rule, ok := rulesByID[status.Rule]
		if !ok || rule.Category != CategoryMigration {
			continue
		}
		if status.Result == "unknown" || status.Result == "skip" || len(status.Blind) > 0 {
			out.Unresolved = append(out.Unresolved, status)
		}
	}
	for _, rule := range Rules() {
		if rule.Category == CategoryMigration && !seen[rule.ID] {
			out.Unresolved = append(out.Unresolved, RuleStatus{Rule: rule.ID, Result: "unknown", Reason: "rule was not evaluated"})
		}
	}
	if len(out.Blockers) > 0 {
		out.Verdict = "blocked"
	} else if len(out.Unresolved) > 0 || len(report.Coverage.BlindContexts) > 0 {
		out.Verdict = "unknown"
	}
	return out
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityCritical:
		return 2
	case SeverityWarning:
		return 1
	default:
		return 0
	}
}
