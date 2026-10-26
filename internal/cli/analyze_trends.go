package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/humanize"
)

// trendFlags select the window of captures a trend reads: from --from, or the
// start of history, up to --run, or the most recent capture.
type trendFlags struct {
	from, run      string
	limit          int
	includePartial bool
}

func (f *trendFlags) add(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.from, "from", "", "first capture in the window: ID, label or date (inclusive)")
	cmd.Flags().StringVar(&f.run, "run", "", "last capture in the window: ID, label, latest, previous or date (default latest)")
	cmd.Flags().IntVar(&f.limit, "limit", 30, "number of complete captures (0 means unlimited)")
	cmd.Flags().BoolVar(&f.includePartial, "include-partial", false, "include partial captures")
}

// windowFlags are the trendFlags that only a trend reads.
var windowFlags = []string{"from", "limit", "include-partial"}

// options resolves trend scope with the same --all-contexts precedence as
// every other stored-evidence command.
func (f trendFlags) options(ctx context.Context, s *assessment.Store, a *App) (assessment.TrendOptions, error) {
	opts := assessment.TrendOptions{Limit: f.limit, IncludePartial: f.includePartial, Contexts: a.StoredContextNames()}
	var err error
	if f.from != "" {
		opts.FromID, err = s.ResolveRun(ctx, f.from)
		if err != nil {
			return opts, fmt.Errorf("--from: %w", err)
		}
	}
	if f.run != "" {
		opts.ToID, err = s.ResolveRun(ctx, f.run)
		if err != nil {
			return opts, fmt.Errorf("--run: %w", err)
		}
	}
	return opts, nil
}

// requireTrend rejects flags that only apply with --trend when it is off.
func requireTrend(cmd *cobra.Command, trend bool, names ...string) error {
	if trend {
		return nil
	}
	for _, name := range names {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--%s applies with --trend", name)
		}
	}
	return nil
}

func newAnalyzeChurnCommand(a *App) *cobra.Command {
	var flags trendFlags
	cmd := &cobra.Command{
		Use:   "churn",
		Short: "Show VM population and churn across the stored captures",
		Example: `  # VM population across the stored history
  vsfleet analyze churn

  # A window, by label, ID or date
  vsfleet analyze churn --from pre-migration --run nightly

  # One context, most recent 10 captures, as JSON
  vsfleet analyze churn -n prod --limit 10 -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.History()
			if err != nil {
				return err
			}
			opts, err := flags.options(cmd.Context(), s, a)
			if err != nil {
				return err
			}
			trend, err := s.ChurnTrend(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if a.json() {
				return writeJSON(a.out(), trend)
			}
			t := newTable(a.out(), "RUN", "DATE", "VMS", "+APPEARED", "-VANISHED", "→MOVED", "~MODIFIED")
			for _, p := range trend.Points {
				t.row(strconv.FormatInt(p.Run.ID, 10), p.Run.StartedAt.Local().Format("2006-01-02"), itoa(p.VMCount), itoa(p.Appeared), itoa(p.Vanished), itoa(p.Moved), itoa(p.Modified))
			}
			t.flush()
			return nil
		},
	}
	flags.add(cmd)
	renameFlags(cmd, flagRename{name: "to", hint: "use --run, the last capture in the window"})
	return cmd
}

func newAnalyzeSnapshotsCommand(a *App) *cobra.Command {
	var (
		flags trendFlags
		older string
		trend bool
	)
	cmd := &cobra.Command{
		Use:   "snapshots",
		Short: "Show snapshot ages in a capture, or their trend across captures",
		Long: strings.TrimSpace(`
List every snapshot in the capture --run names with its age and when the
history first and last saw it. --trend instead counts snapshots, and those
older than --older-than (30 days by default), in every capture of a window.

"vsfleet get snapshots --run RUN" lists the same snapshots as inventory.`),
		Example: `  # Snapshots in the most recent capture
  vsfleet analyze snapshots

  # Only snapshots at least 90 days old, in capture 42, as JSON
  vsfleet analyze snapshots --older-than 90d --run 42 -o json

  # Snapshot totals and stale counts across a window
  vsfleet analyze snapshots --trend --from 2026-01-01 --include-partial`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireTrend(cmd, trend, windowFlags...); err != nil {
				return err
			}
			s, err := a.History()
			if err != nil {
				return err
			}
			if trend {
				if older == "" {
					older = "30d"
				}
				return snapshotTrend(cmd.Context(), a, s, flags, older)
			}
			selector := flags.run
			if selector == "" {
				selector = "latest"
			}
			at, err := s.ResolveRun(cmd.Context(), selector)
			if err != nil {
				return fmt.Errorf("--run: %w", err)
			}
			olderDuration, err := parseHumanDuration(older)
			if err != nil {
				return fmt.Errorf("--older-than: %w", err)
			}
			ages, err := s.SnapshotAgesForContexts(cmd.Context(), at, olderDuration, a.StoredContextNames())
			if err != nil {
				return err
			}
			if a.json() {
				return writeJSON(a.out(), ages)
			}
			t := newTable(a.out(), "VM", "CONTEXT", "SNAPSHOT", "CREATED", "AGE", "FIRST SEEN", "LAST SEEN")
			for _, v := range ages {
				t.row(v.VMName, v.Context, v.Name, v.CreateTime.Local().Format("2006-01-02"), humanize.Age(v.Age), v.FirstSeen.Local().Format("2006-01-02"), v.LastSeen.Local().Format("2006-01-02"))
			}
			t.flush()
			return nil
		},
	}
	flags.add(cmd)
	cmd.Flags().BoolVar(&trend, "trend", false, "count snapshots in every capture of a window instead of listing one capture's")
	cmd.Flags().StringVar(&older, "older-than", "", "only snapshots at least this old (e.g. 30d, 2w); with --trend, the stale threshold (default 30d)")
	renameFlags(cmd,
		flagRename{name: "at", hint: "use --run"},
		flagRename{name: "to", hint: "use --run, the last capture in the window"},
	)
	return cmd
}

func snapshotTrend(ctx context.Context, a *App, s *assessment.Store, flags trendFlags, older string) error {
	opts, err := flags.options(ctx, s, a)
	if err != nil {
		return err
	}
	age, err := parseHumanDuration(older)
	if err != nil {
		return fmt.Errorf("--older-than: %w", err)
	}
	trend, err := s.SnapshotTrend(ctx, opts, age)
	if err != nil {
		return err
	}
	if a.json() {
		return writeJSON(a.out(), trend)
	}
	t := newTable(a.out(), "RUN", "DATE", "TOTAL", "STALE", "OLDEST", "TREND")
	values := make([]float64, len(trend.Points))
	for i, p := range trend.Points {
		values[i] = float64(p.Total)
		t.row(strconv.FormatInt(p.Run.ID, 10), p.Run.StartedAt.Local().Format("2006-01-02"), itoa(p.Total), itoa(p.Stale), humanize.Age(p.OldestAge), "")
	}
	t.flush()
	if len(values) > 0 {
		fmt.Fprintf(a.out(), "snapshot totals: %s\n", sparkline(values))
	}
	return nil
}

// capacityTrend prints compute and storage capacity across a window of
// captures, per kind.
func capacityTrend(ctx context.Context, a *App, s *assessment.Store, flags trendFlags, kind string) error {
	opts, err := flags.options(ctx, s, a)
	if err != nil {
		return err
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "all" && kind != "host" && kind != "cluster" && kind != "datastore" {
		return errors.New("--kind must be host, cluster, datastore, or all")
	}
	kinds := []string{kind}
	if kind == "all" {
		kinds = []string{"host", "cluster", "datastore"}
	}
	trend, err := s.CapacityTrend(ctx, opts, kinds)
	if err != nil {
		return err
	}
	if a.json() {
		return writeJSON(a.out(), trend)
	}
	t := newTable(a.out(), "KIND", "SCOPE", "NAME", "RUN", "CPU CAP", "CPU USED", "CPU%", "MEM CAP", "MEM USED", "MEM%", "STORAGE", "USED", "FREE", "DS%")
	for _, series := range trend.Series {
		for _, p := range series.Points {
			t.row(series.Kind, series.Scope, dash(series.Name), strconv.FormatInt(p.Run.ID, 10), capacityCell(p.CPUCapacity), capacityCell(p.CPUUsed), percentCell(p.CPUUtilization), capacityCell(p.MemoryCapacity), capacityCell(p.MemoryUsed), percentCell(p.MemoryUtilization), capacityCell(p.StorageCapacity), capacityCell(p.StorageUsed), capacityCell(p.StorageFree), percentCell(p.StorageUtilization))
		}
	}
	t.flush()
	return nil
}

// renamedTrendsCommand answers the removed "assessment trends" group, and
// each of its subcommands, with the analysis that replaced it.
func renamedTrendsCommand() *cobra.Command {
	cmd := renamedCommand("trends", nil, `use "vsfleet analyze churn", "analyze snapshots --trend" or "analyze capacity --trend"`)
	generic := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		for _, arg := range args {
			switch arg {
			case "churn":
				return errors.New(`"assessment trends churn" was renamed: use "vsfleet analyze churn"`)
			case "snapshots", "capacity":
				return fmt.Errorf(`"assessment trends %[1]s" was renamed: use "vsfleet analyze %[1]s --trend"`, arg)
			}
		}
		return generic(c, args)
	}
	return cmd
}
