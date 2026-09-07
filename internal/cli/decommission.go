package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/decommission"
)

type decommissionExitError struct{ blockers int }

func (e *decommissionExitError) Error() string {
	return fmt.Sprintf("VM decommission check blocked (%d blocker(s))", e.blockers)
}

func (e *decommissionExitError) ExitCode() int { return 2 }

func newVMDecommissionCheckCommand(a *App) *cobra.Command {
	var failOnBlockers bool
	cmd := &cobra.Command{
		Use:   "decommission-check NAME_OR_UUID [RUN]",
		Short: "Review stored evidence before decommissioning a VM",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			selector := "latest"
			if len(args) == 2 {
				selector = args[1]
			}
			data, err := loadRunExportData(cmd, a, []string{selector})
			if err != nil {
				return err
			}
			result := decommission.Evaluate(data, args[0], a.ContextNames)
			if a.json() {
				if err := writeJSON(a.out(), result); err != nil {
					return err
				}
			} else if err := printDecommissionResult(a.out(), result); err != nil {
				return err
			}
			if failOnBlockers && result.Verdict == decommission.VerdictBlocked {
				return &decommissionExitError{blockers: countBlockers(result)}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&failOnBlockers, "fail-on-blockers", false, "exit 2 when decommission blockers are present")
	return cmd
}

func countBlockers(result decommission.Report) int {
	count := 0
	for _, subject := range result.Subjects {
		for _, check := range subject.Checks {
			if check.Status == decommission.StatusFail && check.Impact == decommission.ImpactBlocker {
				count++
			}
		}
	}
	return count
}

func printDecommissionResult(out io.Writer, result decommission.Report) error {
	fmt.Fprintf(out, "VM decommission check: %s\n", strings.ToUpper(result.Verdict))
	fmt.Fprintf(out, "Assessment: %d  Query: %s\n", result.RunID, result.Query)
	if result.Ambiguous {
		fmt.Fprintf(out, "[AMBIGUOUS] %d distinct VMs matched; use --context to narrow the search.\n", len(result.Subjects))
	}
	for i, subject := range result.Subjects {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "[%s] %s %s\n", strings.ToUpper(subject.Verdict), subject.Subject.Kind, subject.Subject.Name)
		if len(subject.Subject.Members) > 0 {
			contexts := make([]string, 0, len(subject.Subject.Members))
			for _, member := range subject.Subject.Members {
				contexts = append(contexts, member.Context)
			}
			fmt.Fprintf(out, "  Contexts: %s\n", strings.Join(contexts, ", "))
		}
		t := newTable(out, "CHECK", "STATUS", "MESSAGE")
		for _, check := range subject.Checks {
			t.row(check.ID, strings.ToUpper(check.Status), check.Message)
		}
		t.flush()
		if len(subject.Dependencies) > 0 {
			fmt.Fprintln(out, "Dependencies:")
			for _, edge := range subject.Dependencies {
				fmt.Fprintf(out, "  %s/%s --%s--> %s/%s [%s, %s]\n", edge.From.Kind, edge.From.Name, edge.Relation, edge.To.Kind, edge.To.Name, edge.Confidence, edge.Basis)
			}
		}
		for _, unresolved := range subject.Unresolved {
			fmt.Fprintf(out, "  unresolved: %s\n", unresolved)
		}
		for _, blind := range subject.Blind {
			fmt.Fprintf(out, "  blind: %s — %s\n", blind.Context, blind.Reason)
		}
	}
	return nil
}
