package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/health"
)

type healthExitError struct{ findings int }

func (e *healthExitError) Error() string {
	return fmt.Sprintf("health check failed (%d finding(s))", e.findings)
}
func (e *healthExitError) ExitCode() int { return 2 }

type readinessExitError struct{ blockers int }

func (e *readinessExitError) Error() string {
	return fmt.Sprintf("migration readiness blocked (%d finding(s))", e.blockers)
}
func (e *readinessExitError) ExitCode() int { return 2 }

type healthFlags struct {
	maxSnapshotAge                     string
	minDatastoreFree, minGuestDiskFree float64
	disabled                           []string
	minimumSeverity, category          string
	failOnFindings, listRules, wide    bool
}

func (f *healthFlags) add(cmd *cobra.Command, listRules bool) {
	cmd.Flags().StringVar(&f.maxSnapshotAge, "max-snapshot-age", "30d", "report snapshots at least this old (e.g. 30d, 2w)")
	cmd.Flags().Float64Var(&f.minDatastoreFree, "min-datastore-free", 10, "report datastores with less than this percent free space")
	cmd.Flags().Float64Var(&f.minGuestDiskFree, "min-guest-disk-free", 10, "report guest filesystems with less than this percent free space")
	cmd.Flags().StringSliceVar(&f.disabled, "disable-rule", nil, "disable a health rule (repeat or comma-separate)")
	cmd.Flags().StringVar(&f.minimumSeverity, "severity", "info", "minimum severity to report: info, warning, or critical")
	cmd.Flags().StringVar(&f.category, "category", "", "limit findings to a category: migration, availability, security, capacity, or hygiene")
	cmd.Flags().BoolVar(&f.failOnFindings, "fail-on-findings", false, "exit 2 when a reported finding meets --severity")
	cmd.Flags().BoolVar(&f.wide, "wide", false, "include recommendations and structured evidence")
	if listRules {
		cmd.Flags().BoolVar(&f.listRules, "list-rules", false, "list health rules and exit")
	}
}

func newHealthCommand(a *App) *cobra.Command {
	return newFindingsCommand(a, "health [RUN]", "Assess the estate described by a stored assessment")
}

func newFindingsCommand(a *App, use, short string) *cobra.Command {
	var flags healthFlags
	cmd := &cobra.Command{Use: use, Short: short, Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if flags.listRules {
			return printHealthRules(a)
		}
		report, err := evaluateHealthCommand(cmd, a, args, flags)
		if err != nil {
			return err
		}
		for _, rule := range report.Rules {
			if rule.Status == "not-evaluated" || rule.Result == "unknown" {
				reason := rule.Reason
				if len(rule.Blind) > 0 {
					reason = strings.TrimSpace(strings.TrimSuffix(reason, ".") + "; blind contexts: " + strings.Join(rule.Blind, ", "))
				}
				fmt.Fprintf(a.errOut(), "%s %s: %s\n", glyphFail, rule.Rule, reason)
			}
		}
		if a.json() {
			if err := writeJSON(a.out(), report); err != nil {
				return err
			}
		} else {
			printHealthTable(a, report, flags.wide)
		}
		if flags.failOnFindings && report.Counts.Total > 0 {
			return &healthExitError{findings: report.Counts.Total}
		}
		return nil
	}}
	flags.add(cmd, true)
	return cmd
}

func evaluateHealthCommand(cmd *cobra.Command, a *App, args []string, flags healthFlags) (health.Report, error) {
	age, err := parseHumanDuration(flags.maxSnapshotAge)
	if err != nil {
		return health.Report{}, fmt.Errorf("--max-snapshot-age: %w", err)
	}
	if flags.minDatastoreFree < 0 || flags.minDatastoreFree > 100 {
		return health.Report{}, fmt.Errorf("--min-datastore-free must be between 0 and 100")
	}
	if flags.minGuestDiskFree < 0 || flags.minGuestDiskFree > 100 {
		return health.Report{}, fmt.Errorf("--min-guest-disk-free must be between 0 and 100")
	}
	severity, err := parseHealthSeverity(flags.minimumSeverity)
	if err != nil {
		return health.Report{}, err
	}
	category, err := parseHealthCategory(flags.category)
	if err != nil {
		return health.Report{}, err
	}
	disabledIDs, err := validateHealthRules(flags.disabled)
	if err != nil {
		return health.Report{}, err
	}
	selector := "latest"
	if len(args) == 1 {
		selector = args[0]
	}
	data, err := loadRunExportData(cmd, a, []string{selector})
	if err != nil {
		return health.Report{}, err
	}
	report := health.Evaluate(data, health.Options{Thresholds: health.Thresholds{SnapshotAge: age, DatastoreFreePct: flags.minDatastoreFree, GuestDiskFreePct: flags.minGuestDiskFree}, Disabled: disabledIDs})
	return filterHealthReport(report, severity, category), nil
}

func loadRunExportData(cmd *cobra.Command, a *App, args []string) (assessment.ExportData, error) {
	selector := "latest"
	if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
		selector = args[0]
	}
	s, err := a.History()
	if err != nil {
		return assessment.ExportData{}, err
	}
	runID, err := s.ResolveRun(cmd.Context(), selector)
	if err != nil {
		return assessment.ExportData{}, err
	}
	return s.LoadExportData(cmd.Context(), runID)
}

func printHealthRules(a *App) error {
	if a.json() {
		rules := make([]map[string]any, 0, len(health.Rules()))
		for _, rule := range health.Rules() {
			rules = append(rules, map[string]any{"rule": rule.ID, "category": rule.Category, "severity": rule.Severity, "summary": rule.Summary, "min_schema": rule.MinSchema, "needs": rule.Needs})
		}
		return writeJSON(a.out(), rules)
	}
	t := newTable(a.out(), "RULE", "CATEGORY", "SEVERITY", "MIN SCHEMA", "SUMMARY")
	for _, rule := range health.Rules() {
		minSchema := "-"
		if rule.MinSchema > 0 {
			minSchema = strconv.Itoa(rule.MinSchema)
		}
		t.row(rule.ID, string(rule.Category), string(rule.Severity), minSchema, rule.Summary)
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

func parseHealthCategory(value string) (health.Category, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	category := health.Category(strings.ToLower(strings.TrimSpace(value)))
	for _, rule := range health.Rules() {
		if rule.Category == category {
			return category, nil
		}
	}
	return "", fmt.Errorf("unknown --category %q (supported: migration, availability, security, capacity, hygiene)", value)
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

func filterHealthReport(report health.Report, minimum health.Severity, category health.Category) health.Report {
	minRank := healthSeverityRank(minimum)
	filtered := make([]health.Finding, 0, len(report.Findings))
	for _, finding := range report.Findings {
		if healthSeverityRank(finding.Severity) >= minRank && (category == "" || finding.Category == category) {
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

func printHealthTable(a *App, report health.Report, wide bool) {
	fmt.Fprintf(a.out(), "Assessment %d: %d finding(s) (%d info, %d warning, %d critical)\n", report.RunID, report.Counts.Total, report.Counts.Info, report.Counts.Warning, report.Counts.Critical)
	headers := []string{"SEVERITY", "CATEGORY", "RULE", "OBJECT", "CONTEXT", "MESSAGE"}
	if wide {
		headers = append(headers, "RECOMMENDATION", "EVIDENCE")
	}
	t := newTable(a.out(), headers...)
	for _, finding := range report.Findings {
		row := []string{string(finding.Severity), string(finding.Category), finding.Rule, finding.Object.Kind + "/" + finding.Object.Name, finding.Object.Context, finding.Message}
		if wide {
			row = append(row, finding.Recommendation, renderEvidence(finding.Evidence))
		}
		t.row(row...)
	}
	t.flush()
	unevaluated := make([]string, 0)
	for _, rule := range report.Rules {
		if rule.Status == "not-evaluated" || rule.Result == "unknown" {
			unevaluated = append(unevaluated, rule.Rule)
		}
	}
	if len(unevaluated) > 0 {
		sort.Strings(unevaluated)
		fmt.Fprintf(a.out(), "Rules not evaluated: %s\n", strings.Join(unevaluated, ", "))
	}
}

func renderEvidence(evidence []health.Evidence) string {
	parts := make([]string, 0, len(evidence))
	for _, item := range evidence {
		value := item.Field + "=" + item.Observed
		if item.Expected != "" {
			value += " (expected " + item.Expected + ")"
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, "; ")
}
