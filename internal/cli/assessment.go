package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
)

func newAssessmentCommand(a *App) *cobra.Command {
	cmd := &cobra.Command{Use: "assessment", Aliases: []string{"assess", "history"}, Short: "Capture and compare VM assessments"}
	cmd.AddCommand(newAssessmentRunCommand(a), newAssessmentListCommand(a), newAssessmentDiffCommand(a), newAssessmentSnapshotsCommand(a), newAssessmentDeleteCommand(a))
	return cmd
}

func newAssessmentRunCommand(a *App) *cobra.Command {
	return &cobra.Command{Use: "run", Short: "Capture a point-in-time VM assessment", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		contexts, err := a.Contexts()
		if err != nil {
			return err
		}
		service, err := a.Assessment()
		if err != nil {
			return err
		}
		run, err := service.Capture(cmd.Context(), assessment.CaptureOptions{Contexts: contexts, Source: "cli", Progress: func(p assessment.ContextProgress) {
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
		t := newTable(a.out(), "ID", "STARTED", "STATUS", "CONTEXTS", "SUCCESSFUL")
		for _, r := range runs {
			t.row(strconv.FormatInt(r.ID, 10), r.StartedAt.Local().Format("2006-01-02 15:04:05"), string(r.Status), itoa(r.RequestedContexts), itoa(r.SuccessfulContexts))
		}
		t.flush()
		return nil
	}}
}

func newAssessmentDiffCommand(a *App) *cobra.Command {
	var runtime bool
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
		if a.json() {
			return writeJSON(a.out(), d)
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
		return nil
	}}
	cmd.Flags().BoolVar(&runtime, "include-runtime", false, "include volatile power, guest, IP, tools, and storage changes")
	return cmd
}

func newAssessmentSnapshotsCommand(a *App) *cobra.Command {
	var at int64
	var older time.Duration
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
		ages, err := s.SnapshotAges(cmd.Context(), at, older)
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
	cmd.Flags().DurationVar(&older, "older-than", 0, "only snapshots at least this old")
	return cmd
}

func newAssessmentDeleteCommand(a *App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{Use: "delete RUN", Short: "Delete one stored assessment", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("assessment ID %q is not a number", args[0])
		}
		if !force {
			return fmt.Errorf("deleting assessment history requires --force")
		}
		s, err := a.History()
		if err != nil {
			return err
		}
		if err := s.DeleteRun(cmd.Context(), id); err != nil {
			return err
		}
		fmt.Fprintf(a.out(), "deleted assessment %d\n", id)
		return nil
	}}
	cmd.Flags().BoolVar(&force, "force", false, "confirm deletion")
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
		return 0, fmt.Errorf("assessment selector %q is not a number, latest, or previous", v)
	}
	for _, r := range runs {
		if r.ID == id {
			return id, nil
		}
	}
	return 0, fmt.Errorf("assessment %d not found", id)
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
	fmt.Fprintf(out, "assessment %d: %s (%d/%d contexts successful)\n", r.ID, r.Status, r.SuccessfulContexts, r.RequestedContexts)
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
