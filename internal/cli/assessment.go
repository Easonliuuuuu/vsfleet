package cli

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/version"
)

type policyExitError struct{ violations []assessment.PolicyViolation }

func (e *policyExitError) Error() string {
	return fmt.Sprintf("assessment diff policy failed (%d violation(s))", len(e.violations))
}
func (e *policyExitError) ExitCode() int { return 2 }

func newAssessmentCommand(a *App) *cobra.Command {
	cmd := &cobra.Command{Use: "assessment", Aliases: []string{"assess", "history"}, Short: "Capture and compare VM assessments"}
	cmd.AddCommand(newAssessmentRunCommand(a), newAssessmentListCommand(a), newAssessmentDiffCommand(a), newAssessmentSnapshotsCommand(a), newAssessmentDeleteCommand(a), newAssessmentUpdateCommand(a))
	return cmd
}

func newAssessmentRunCommand(a *App) *cobra.Command {
	var label, note string
	var pin bool
	cmd := &cobra.Command{Use: "run", Short: "Capture a point-in-time VM assessment", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		contexts, err := a.Contexts()
		if err != nil {
			return err
		}
		service, err := a.Assessment()
		if err != nil {
			return err
		}
		run, err := service.Capture(cmd.Context(), assessment.CaptureOptions{Contexts: contexts, Source: "cli", Label: label, Note: note, Pinned: pin, ToolVersion: version.String(), InventorySchemaVersion: "1", Progress: func(p assessment.ContextProgress) {
			if p.Error != nil {
				fmt.Fprintf(a.errOut(), "%s %s: %v\n", glyphFail, p.Context, p.Error)
			}
		}})
		if err != nil {
			return err
		}
		if a.json() {
			if writeErr := writeJSON(a.out(), run); writeErr != nil {
				return writeErr
			}
		} else {
			printRun(a.out(), run)
		}
		if run.Status == assessment.RunFailed {
			return fmt.Errorf("assessment %d failed: no context returned VM inventory", run.ID)
		}
		return nil
	}}
	cmd.Flags().StringVar(&label, "label", "", "stable label for this assessment (used as a selector)")
	cmd.Flags().StringVar(&note, "note", "", "operator note stored with this assessment")
	cmd.Flags().BoolVar(&pin, "pin", false, "pin this assessment against deletion")
	return cmd
}

func newAssessmentListCommand(a *App) *cobra.Command {
	return &cobra.Command{Use: "list", Aliases: []string{"ls"}, Short: "List stored assessments", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := a.History()
		if err != nil {
			return err
		}
		runs, err := s.Runs(cmd.Context())
		if err != nil {
			return err
		}
		if a.json() {
			return writeJSON(a.out(), runs)
		}
		t := newTable(a.out(), "ID", "LABEL", "STARTED", "STATUS", "CONTEXTS", "SUCCESSFUL")
		for _, r := range runs {
			label := r.Label
			if r.Pinned {
				label = "📌 " + label
			}
			t.row(strconv.FormatInt(r.ID, 10), label, r.StartedAt.Local().Format("2006-01-02 15:04:05"), string(r.Status), itoa(r.RequestedContexts), itoa(r.SuccessfulContexts))
		}
		t.flush()
		return nil
	}}
}

func newAssessmentDiffCommand(a *App) *cobra.Command {
	var runtime bool
	var failOn []string
	var maxAge string
	var requireComplete bool
	cmd := &cobra.Command{Use: "diff [BASE] [TARGET]", Short: "Compare two assessments", Args: cobra.MaximumNArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := a.History()
		if err != nil {
			return err
		}
		base, target, err := resolveRunPair(cmd.Context(), s, args)
		if err != nil {
			return err
		}
		d, err := s.Diff(cmd.Context(), base, target, runtime)
		if err != nil {
			return err
		}
		age, err := parseHumanDuration(maxAge)
		if err != nil {
			return fmt.Errorf("--max-snapshot-age: %w", err)
		}
		for _, rule := range failOn {
			if !validPolicyRule(rule) {
				return fmt.Errorf("unknown --fail-on rule %q", rule)
			}
		}
		if len(failOn) > 0 || age > 0 || requireComplete {
			policy := assessment.EvaluatePolicy(d, assessment.PolicyOptions{FailOn: failOn, MaxSnapshotAge: age, RequireComplete: requireComplete})
			d.Policy = &policy
		}
		if a.json() {
			if err := writeJSON(a.out(), d); err != nil {
				return err
			}
			if d.Policy != nil && !d.Policy.Passed {
				return &policyExitError{violations: d.Policy.Violations}
			}
			return nil
		}
		fmt.Fprintf(a.out(), "Assessment %d → %d  (%s → %s)\n", d.Base.ID, d.Target.ID, d.Base.Status, d.Target.Status)
		fmt.Fprintf(a.out(), "Appeared: %d  Vanished: %d  Moved: %d  Modified: %d  Snapshots: %d\n", d.Counts.Appeared, d.Counts.Vanished, d.Counts.Moved, d.Counts.Modified, d.Counts.Snapshots)
		t := newTable(a.out(), "CHANGE", "VM", "CONTEXT", "DETAIL")
		for _, v := range d.VMs {
			t.row(strings.Join(v.Changes, ","), v.Name, v.Context, changeDetail(v))
		}
		for _, v := range d.Snapshots {
			t.row("snapshot "+v.Kind, v.VMName, v.Context, v.Name)
		}
		t.flush()
		for _, w := range d.Warnings {
			fmt.Fprintf(a.errOut(), "%s %s\n", glyphFail, w)
		}
		if d.Policy != nil {
			if d.Policy.Passed {
				fmt.Fprintln(a.out(), "policy: PASS")
			} else {
				for _, v := range d.Policy.Violations {
					fmt.Fprintf(a.out(), "policy: FAIL [%s] %s\n", v.Rule, v.Message)
				}
				return &policyExitError{violations: d.Policy.Violations}
			}
		}
		return nil
	}}
	cmd.Flags().BoolVar(&runtime, "include-runtime", false, "include volatile power, guest, IP, tools, and storage changes")
	cmd.Flags().StringSliceVar(&failOn, "fail-on", nil, "fail with exit code 2 when a change category appears (repeat or comma-separate)")
	cmd.Flags().StringVar(&maxAge, "max-snapshot-age", "", "fail when a target snapshot is at least this old (e.g. 30d, 2w)")
	cmd.Flags().BoolVar(&requireComplete, "require-complete", false, "fail unless both runs are complete and fully comparable")
	return cmd
}

func newAssessmentSnapshotsCommand(a *App) *cobra.Command {
	var at int64
	var older string
	cmd := &cobra.Command{Use: "snapshots", Short: "Show snapshot ages in an assessment", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := a.History()
		if err != nil {
			return err
		}
		if at == 0 {
			at, err = resolveRunPairTarget(cmd.Context(), s)
			if err != nil {
				return err
			}
		}
		olderDuration, err := parseHumanDuration(older)
		if err != nil {
			return fmt.Errorf("--older-than: %w", err)
		}
		ages, err := s.SnapshotAges(cmd.Context(), at, olderDuration)
		if err != nil {
			return err
		}
		if a.json() {
			return writeJSON(a.out(), ages)
		}
		t := newTable(a.out(), "VM", "CONTEXT", "SNAPSHOT", "CREATED", "AGE", "FIRST SEEN", "LAST SEEN")
		for _, v := range ages {
			t.row(v.VMName, v.Context, v.Name, v.CreateTime.Local().Format("2006-01-02"), humanDuration(v.Age), v.FirstSeen.Local().Format("2006-01-02"), v.LastSeen.Local().Format("2006-01-02"))
		}
		t.flush()
		return nil
	}}
	cmd.Flags().Int64Var(&at, "at", 0, "assessment ID (default: latest)")
	cmd.Flags().StringVar(&older, "older-than", "", "only snapshots at least this old (e.g. 30d, 2w)")
	return cmd
}

func newAssessmentDeleteCommand(a *App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{Use: "delete RUN", Short: "Delete one stored assessment", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !force {
			return fmt.Errorf("deleting assessment history requires --force")
		}
		s, err := a.History()
		if err != nil {
			return err
		}
		if err := s.DeleteRunSelector(cmd.Context(), args[0]); err != nil {
			return err
		}
		fmt.Fprintf(a.out(), "deleted assessment %s\n", args[0])
		return nil
	}}
	cmd.Flags().BoolVar(&force, "force", false, "confirm deletion")
	return cmd
}

func newAssessmentUpdateCommand(a *App) *cobra.Command {
	var label, note string
	var pin, unpin bool
	cmd := &cobra.Command{Use: "update RUN", Short: "Update assessment label, note, or pin", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if pin && unpin {
			return fmt.Errorf("--pin and --unpin cannot be combined")
		}
		if !cmd.Flags().Changed("label") && !cmd.Flags().Changed("note") && !pin && !unpin {
			return fmt.Errorf("at least one metadata change is required")
		}
		s, err := a.History()
		if err != nil {
			return err
		}
		var lp, np *string
		var pp *bool
		if cmd.Flags().Changed("label") {
			lp = &label
		}
		if cmd.Flags().Changed("note") {
			np = &note
		}
		if pin || unpin {
			v := pin
			pp = &v
		}
		run, err := s.UpdateRunFields(cmd.Context(), args[0], lp, np, pp)
		if err != nil {
			return err
		}
		if a.json() {
			return writeJSON(a.out(), run)
		}
		printRun(a.out(), run)
		return nil
	}}
	cmd.Flags().StringVar(&label, "label", "", "set the run label (empty clears it)")
	cmd.Flags().StringVar(&note, "note", "", "set the run note (empty clears it)")
	cmd.Flags().BoolVar(&pin, "pin", false, "pin the run")
	cmd.Flags().BoolVar(&unpin, "unpin", false, "unpin the run")
	return cmd
}

func resolveRunPair(ctx context.Context, s *assessment.Store, args []string) (int64, int64, error) {
	runs, err := s.Runs(ctx)
	if err != nil {
		return 0, 0, err
	}
	if len(runs) < 2 {
		return 0, 0, fmt.Errorf("need at least two assessments to compare")
	}
	if len(args) == 0 {
		return runs[1].ID, runs[0].ID, nil
	}
	if len(args) == 1 {
		target, err := runSelector(args[0], runs)
		if err != nil {
			return 0, 0, err
		}
		for _, r := range runs {
			if r.ID != target && r.StartedAt.Before(targetTime(runs, target)) {
				return r.ID, target, nil
			}
		}
		return 0, 0, fmt.Errorf("no baseline assessment before %s", args[0])
	}
	base, err := runSelector(args[0], runs)
	if err != nil {
		return 0, 0, err
	}
	target, err := runSelector(args[1], runs)
	if err != nil {
		return 0, 0, err
	}
	return base, target, nil
}
func resolveRunPairTarget(ctx context.Context, s *assessment.Store) (int64, error) {
	runs, err := s.Runs(ctx)
	if err != nil {
		return 0, err
	}
	if len(runs) == 0 {
		return 0, fmt.Errorf("no assessments stored")
	}
	return runs[0].ID, nil
}
func runSelector(v string, runs []assessment.Run) (int64, error) {
	if v == "latest" {
		return runs[0].ID, nil
	}
	if v == "previous" {
		if len(runs) < 2 {
			return 0, fmt.Errorf("no previous assessment")
		}
		return runs[1].ID, nil
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		for _, r := range runs {
			if r.Label != "" && strings.EqualFold(r.Label, v) {
				return r.ID, nil
			}
		}
		return 0, fmt.Errorf("assessment selector %q is not a number, label, latest, or previous", v)
	}
	for _, r := range runs {
		if r.ID == id {
			return id, nil
		}
	}
	return 0, fmt.Errorf("assessment %d not found", id)
}

var durationToken = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)([smhdw])`)

func parseHumanDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	if d, err := time.ParseDuration(value); err == nil {
		return d, nil
	}
	matches := durationToken.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("invalid duration %q", value)
	}
	var total time.Duration
	consumed := ""
	for _, m := range matches {
		consumed += m[0]
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, err
		}
		unit := map[string]float64{"s": float64(time.Second), "m": float64(time.Minute), "h": float64(time.Hour), "d": float64(24 * time.Hour), "w": float64(7 * 24 * time.Hour)}[strings.ToLower(m[2])]
		total += time.Duration(n * unit)
	}
	if consumed != value {
		return 0, fmt.Errorf("invalid duration %q", value)
	}
	return total, nil
}

func validPolicyRule(rule string) bool {
	switch rule {
	case "appeared", "vanished", "moved", "modified", "snapshot-created", "snapshot-removed", "snapshot-changed":
		return true
	}
	return false
}

func targetTime(runs []assessment.Run, id int64) time.Time {
	for _, r := range runs {
		if r.ID == id {
			return r.StartedAt
		}
	}
	return time.Time{}
}
func printRun(out interface{ Write([]byte) (int, error) }, r assessment.Run) {
	label := ""
	if r.Label != "" {
		label = " label=" + r.Label
	}
	if r.Pinned {
		label += " pinned"
	}
	fmt.Fprintf(out, "assessment %d: %s%s (%d/%d contexts successful)\n", r.ID, r.Status, label, r.SuccessfulContexts, r.RequestedContexts)
}
func changeDetail(v assessment.VMChange) string {
	if len(v.Fields) == 0 {
		return ""
	}
	parts := make([]string, len(v.Fields))
	for i, f := range v.Fields {
		parts[i] = f.Field + ":" + f.Before + "→" + f.After
	}
	return strings.Join(parts, " ")
}
