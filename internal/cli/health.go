package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/health"
)

type healthExitError struct{ findings int }

func (e *healthExitError) Error() string {
	return fmt.Sprintf("health check failed (%d finding(s))", e.findings)
}

func (e *healthExitError) ExitCode() int { return 2 }

func newHealthCommand(a *App) *cobra.Command {
	var maxSnapshotAge string
	var minDatastoreFree, minGuestDiskFree float64
	var disabled []string
	var minimumSeverity string
	var failOnFindings, listRules bool

	cmd := &cobra.Command{
		Use:   "health [RUN]",
		Short: "Assess the estate described by a stored assessment",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if listRules {
				return printHealthRules(a)
			}
			age, err := parseHumanDuration(maxSnapshotAge)
			if err != nil {
				return fmt.Errorf("--max-snapshot-age: %w", err)
			}
			if minDatastoreFree < 0 || minDatastoreFree > 100 {
				return fmt.Errorf("--min-datastore-free must be between 0 and 100")
			}
			if minGuestDiskFree < 0 || minGuestDiskFree > 100 {
				return fmt.Errorf("--min-guest-disk-free must be between 0 and 100")
			}
			severity, err := parseHealthSeverity(minimumSeverity)
			if err != nil {
				return err
			}
			disabledIDs, err := validateHealthRules(disabled)
			if err != nil {
				return err
			}
			selector := "latest"
			if len(args) == 1 {
				selector = args[0]
			}
			s, err := a.History()
			if err != nil {
				return err
			}
			runID, err := s.ResolveRun(cmd.Context(), selector)
			if err != nil {
				return err
			}
			data, err := s.LoadExportData(cmd.Context(), runID)
			if err != nil {
				return err
			}
			report := health.Evaluate(data, health.Options{
				Thresholds: health.Thresholds{SnapshotAge: age, DatastoreFreePct: minDatastoreFree, GuestDiskFreePct: minGuestDiskFree},
				Disabled:   disabledIDs,
			})
			report = filterHealthReport(report, severity)
			for _, rule := range report.Rules {
				if rule.Status == "not-evaluated" {
					fmt.Fprintf(a.errOut(), "%s %s: %s\n", glyphFail, rule.Rule, rule.Reason)
				}
			}
			if a.json() {
				if err := writeJSON(a.out(), report); err != nil {
					return err
				}
			} else {
				printHealthTable(a, report)
			}
			if failOnFindings && report.Counts.Total > 0 {
				return &healthExitError{findings: report.Counts.Total}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&maxSnapshotAge, "max-snapshot-age", "30d", "report snapshots at least this old (e.g. 30d, 2w)")
	cmd.Flags().Float64Var(&minDatastoreFree, "min-datastore-free", 10, "report datastores with less than this percent free space")
	cmd.Flags().Float64Var(&minGuestDiskFree, "min-guest-disk-free", 10, "report guest filesystems with less than this percent free space")
	cmd.Flags().StringSliceVar(&disabled, "disable-rule", nil, "disable a health rule (repeat or comma-separate)")
	cmd.Flags().StringVar(&minimumSeverity, "severity", "info", "minimum severity to report: info, warning, or critical")
	cmd.Flags().BoolVar(&failOnFindings, "fail-on-findings", false, "exit 2 when a reported finding meets --severity")
	cmd.Flags().BoolVar(&listRules, "list-rules", false, "list health rules and exit")
	return cmd
}

func printHealthRules(a *App) error {
	if a.json() {
		rules := make([]map[string]any, 0, len(health.Rules()))
		for _, rule := range health.Rules() {
			rules = append(rules, map[string]any{"rule": rule.ID, "severity": rule.Severity, "summary": rule.Summary, "min_schema": rule.MinSchema, "needs": rule.Needs})
		}
		return writeJSON(a.out(), rules)
	}
	t := newTable(a.out(), "RULE", "SEVERITY", "MIN SCHEMA", "SUMMARY")
	for _, rule := range health.Rules() {
		minSchema := "-"
		if rule.MinSchema > 0 {
			minSchema = strconv.Itoa(rule.MinSchema)
		}
		t.row(rule.ID, string(rule.Severity), minSchema, rule.Summary)
	}
	t.flush()
	return nil
}

func parseHealthSeverity(value string) (health.Severity, error) {
	switch health.Severity(strings.ToLower(strings.TrimSpace(value))) {
	case health.SeverityInfo:
		return health.SeverityInfo, nil
	case health.SeverityWarning:
		return health.SeverityWarning, nil
	case health.SeverityCritical:
		return health.SeverityCritical, nil
	default:
		return "", fmt.Errorf("unknown --severity %q (supported: info, warning, critical)", value)
	}
}

func validateHealthRules(values []string) ([]string, error) {
	valid := make(map[string]bool, len(health.Rules()))
	for _, rule := range health.Rules() {
		valid[rule.ID] = true
	}
	seen := make(map[string]bool)
	ids := make([]string, 0, len(values))
	for _, value := range values {
		for _, id := range strings.Split(value, ",") {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if !valid[id] {
				return nil, fmt.Errorf("unknown --disable-rule %q", id)
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids, nil
}

func healthSeverityRank(severity health.Severity) int {
	switch severity {
	case health.SeverityCritical:
		return 2
	case health.SeverityWarning:
		return 1
	default:
		return 0
	}
}

func filterHealthReport(report health.Report, minimum health.Severity) health.Report {
	minRank := healthSeverityRank(minimum)
	filtered := make([]health.Finding, 0, len(report.Findings))
	for _, finding := range report.Findings {
		if healthSeverityRank(finding.Severity) >= minRank {
			filtered = append(filtered, finding)
		}
	}
	report.Findings = filtered
	report.Counts = health.Counts{}
	for _, finding := range filtered {
		switch finding.Severity {
		case health.SeverityInfo:
			report.Counts.Info++
		case health.SeverityWarning:
			report.Counts.Warning++
		case health.SeverityCritical:
			report.Counts.Critical++
		}
	}
	report.Counts.Total = len(filtered)
	return report
}

func printHealthTable(a *App, report health.Report) {
	fmt.Fprintf(a.out(), "Assessment %d: %d finding(s) (%d info, %d warning, %d critical)\n", report.RunID, report.Counts.Total, report.Counts.Info, report.Counts.Warning, report.Counts.Critical)
	t := newTable(a.out(), "SEVERITY", "RULE", "OBJECT", "CONTEXT", "MESSAGE")
	for _, finding := range report.Findings {
		t.row(string(finding.Severity), finding.Rule, finding.Object.Kind+"/"+finding.Object.Name, finding.Object.Context, finding.Message)
	}
	t.flush()
	unevaluated := make([]string, 0)
	for _, rule := range report.Rules {
		if rule.Status == "not-evaluated" {
			unevaluated = append(unevaluated, rule.Rule)
		}
	}
	if len(unevaluated) > 0 {
		sort.Strings(unevaluated)
		fmt.Fprintf(a.out(), "Rules not evaluated: %s\n", strings.Join(unevaluated, ", "))
	}
}
