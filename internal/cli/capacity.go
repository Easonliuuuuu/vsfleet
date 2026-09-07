package cli

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/humanize"
)

type capacityExitError struct{ datastores int }

func (e *capacityExitError) Error() string {
	return fmt.Sprintf("capacity projection threshold reached for %d datastore(s)", e.datastores)
}

func (e *capacityExitError) ExitCode() int { return 2 }

type capacityFlags struct {
	trendFlags
	since            string
	datastores       []string
	minFree          float64
	minFreeBytes     string
	top              int
	failOnProjection string
}

func newAssessmentCapacityCommand(a *App) *cobra.Command {
	var flags capacityFlags
	cmd := &cobra.Command{
		Use:   "capacity [RUN]",
		Short: "Attribute datastore growth and project free-space thresholds",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.History()
			if err != nil {
				return err
			}
			selector, err := capacitySelector(args, flags.to, cmd.Flags().Changed("to"))
			if err != nil {
				return err
			}
			asOf := assessment.Run{}
			if selector != "" {
				id, resolveErr := s.ResolveRun(cmd.Context(), selector)
				if resolveErr != nil {
					return resolveErr
				}
				asOf, err = s.GetRun(cmd.Context(), id)
				if err != nil {
					return err
				}
				flags.to = strconv.FormatInt(id, 10)
			}
			opts, err := flags.trendFlags.options(cmd.Context(), s, a.ContextNames)
			if err != nil {
				return err
			}
			if flags.since != "" {
				duration, parseErr := parseHumanDuration(flags.since)
				if parseErr != nil || duration <= 0 {
					if parseErr == nil {
						parseErr = fmt.Errorf("duration must be greater than zero")
					}
					return fmt.Errorf("--since: %w", parseErr)
				}
				base := time.Now().UTC()
				if !asOf.StartedAt.IsZero() {
					base = asOf.StartedAt
				}
				opts.Since = base.Add(-duration)
			}
			if flags.minFree < 0 || flags.minFree > 100 {
				return fmt.Errorf("--min-free must be between 0 and 100")
			}
			minFreeBytes, parseErr := parseHumanBytes(flags.minFreeBytes)
			if parseErr != nil {
				return fmt.Errorf("--min-free-bytes: %w", parseErr)
			}
			if minFreeBytes < 0 {
				return fmt.Errorf("--min-free-bytes must be zero or greater")
			}
			if flags.top < 0 {
				return fmt.Errorf("--top must be zero or greater")
			}
			var failWindow time.Duration
			if flags.failOnProjection != "" {
				failWindow, parseErr = parseHumanDuration(flags.failOnProjection)
				if parseErr != nil || failWindow < 0 {
					if parseErr == nil {
						parseErr = fmt.Errorf("duration must be zero or greater")
					}
					return fmt.Errorf("--fail-on-projection: %w", parseErr)
				}
			}
			report, err := s.CapacityReport(cmd.Context(), opts, assessment.CapacityThresholds{FreePercent: flags.minFree, FreeBytes: minFreeBytes})
			if err != nil {
				return err
			}
			filterCapacityReport(&report, flags.datastores, flags.top)
			printCapacityBlindness(a.errOut(), report)
			if a.json() {
				if err := writeJSON(a.out(), report); err != nil {
					return err
				}
			} else {
				printCapacityReport(a.out(), report)
			}
			if flags.failOnProjection != "" {
				cutoff := failWindow.Hours() / 24
				count := 0
				for _, datastore := range report.Datastores {
					if datastore.Projection == nil || datastore.Projection.DaysRemaining == nil || datastore.Projection.Confidence == assessment.ProjectionUnknown {
						continue
					}
					if *datastore.Projection.DaysRemaining <= cutoff {
						count++
					}
				}
				if count > 0 {
					return &capacityExitError{datastores: count}
				}
			}
			return nil
		},
	}
	flags.trendFlags.add(cmd)
	cmd.Flags().StringVar(&flags.since, "since", "", "include assessments from this time window (e.g. 30d)")
	cmd.Flags().StringSliceVar(&flags.datastores, "datastore", nil, "only show datastore name(s)")
	cmd.Flags().Float64Var(&flags.minFree, "min-free", 10, "project against this minimum free-space percentage")
	cmd.Flags().StringVar(&flags.minFreeBytes, "min-free-bytes", "", "project against this minimum free-space floor")
	cmd.Flags().IntVar(&flags.top, "top", 5, "contributors to show per datastore (0 means all)")
	cmd.Flags().StringVar(&flags.failOnProjection, "fail-on-projection", "", "exit 2 when a floor is projected within this duration")
	return cmd
}

func capacitySelector(args []string, to string, toChanged bool) (string, error) {
	if len(args) == 1 && toChanged {
		return "", fmt.Errorf("RUN and --to cannot both be specified")
	}
	if len(args) == 1 {
		return args[0], nil
	}
	if to != "" {
		return to, nil
	}
	return "latest", nil
}

func filterCapacityReport(report *assessment.CapacityReport, names []string, top int) {
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[strings.ToLower(strings.TrimSpace(name))] = true
	}
	filtered := report.Datastores[:0]
	for _, datastore := range report.Datastores {
		if len(allowed) > 0 && !allowed[strings.ToLower(datastore.Object.Name)] {
			continue
		}
		if top > 0 && len(datastore.Contributors) > top {
			sort.SliceStable(datastore.Contributors, func(i, j int) bool {
				return math.Abs(datastore.Contributors[i].DeltaBytes) > math.Abs(datastore.Contributors[j].DeltaBytes)
			})
			datastore.Contributors = datastore.Contributors[:top]
		}
		filtered = append(filtered, datastore)
	}
	report.Datastores = filtered
}

func printCapacityBlindness(out io.Writer, report assessment.CapacityReport) {
	seen := make(map[string]bool)
	for _, issue := range report.Coverage {
		message := issue.Context + " — " + issue.Message
		if !seen[message] {
			fmt.Fprintf(out, "%s %s\n", glyphFail, message)
			seen[message] = true
		}
	}
	for _, datastore := range report.Datastores {
		for _, blind := range datastore.Blind {
			message := datastore.Object.Name + ": " + blind.Context + " — " + blind.Reason
			if !seen[message] {
				fmt.Fprintf(out, "%s %s\n", glyphFail, message)
				seen[message] = true
			}
		}
	}
}

func printCapacityReport(out io.Writer, report assessment.CapacityReport) {
	if len(report.Datastores) == 0 {
		fmt.Fprintln(out, "No datastore capacity evidence.")
		return
	}
	for i, datastore := range report.Datastores {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "%s  (%s)\n", datastore.Object.Name, datastore.Object.Context)
		fields := newFields(out)
		free := capacityHumanFloat(valueOrZero(datastore.FreeBytes))
		capacity := capacityHumanFloat(valueOrZero(datastore.CapacityBytes))
		fields.add("Free", fmt.Sprintf("%s / %s (%s)", free, capacity, percentText(datastore.FreePercent)))
		if datastore.UsedGrowthBytes != nil {
			fields.add("Growth", fmt.Sprintf("%s over %.1f days", capacityHumanFloat(*datastore.UsedGrowthBytes), datastore.SpanDays))
		}
		fields.add("Confidence", string(datastore.Confidence))
		if datastore.Projection != nil {
			projection := datastore.Projection
			when := "unknown"
			if projection.CrossesAt != nil {
				when = projection.CrossesAt.Local().Format("2006-01-02")
			}
			fields.add("Projected", fmt.Sprintf("%s (%s, R²=%.2f)", when, projection.Confidence, projection.RSquared))
			if len(projection.Reasons) > 0 {
				fields.add("Projection note", strings.Join(projection.Reasons, "; "))
			}
		}
		if len(datastore.Reasons) > 0 {
			fields.add("Reason", strings.Join(datastore.Reasons, "; "))
		}
		fields.flush()
		if len(datastore.Contributors) > 0 {
			fmt.Fprintln(out, "  Largest contributors:")
			table := newTable(out, "NAME", "DELTA", "BASIS", "CONTEXT")
			for _, contributor := range datastore.Contributors {
				table.row(contributor.Name, capacityHumanFloat(contributor.DeltaBytes), string(contributor.Basis), contributor.Context)
			}
			table.flush()
		}
	}
}

func valueOrZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func percentText(value *float64) string {
	if value == nil {
		return "unknown"
	}
	return fmt.Sprintf("%.1f%%", *value)
}

func capacityHumanFloat(value float64) string {
	if value < 0 {
		return "-" + humanize.Bytes(int64(math.Abs(value)))
	}
	return humanize.Bytes(int64(value))
}
