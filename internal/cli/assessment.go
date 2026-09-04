package cli

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/report"
	"github.com/easonliuuuuu/vsfleet/internal/version"
)

type policyExitError struct{ violations []assessment.PolicyViolation }

func (e *policyExitError) Error() string {
	return fmt.Sprintf("assessment diff policy failed (%d violation(s))", len(e.violations))
}
func (e *policyExitError) ExitCode() int { return 2 }

type doctorExitError struct{ message string }

func (e *doctorExitError) Error() string { return e.message }
func (e *doctorExitError) ExitCode() int { return 1 }

func newAssessmentCommand(a *App) *cobra.Command {
	cmd := &cobra.Command{Use: "assessment", Aliases: []string{"assess", "history"}, Short: "Capture and compare historical assessments"}
	cmd.AddCommand(newAssessmentRunCommand(a), newAssessmentListCommand(a), newAssessmentDiffCommand(a), newAssessmentSnapshotsCommand(a), newAssessmentDeleteCommand(a), newAssessmentUpdateCommand(a), newAssessmentTrendsCommand(a), newAssessmentReportCommand(a), newAssessmentExportCommand(a), newAssessmentPruneCommand(a), newAssessmentBackupCommand(a), newAssessmentRestoreCommand(a), newAssessmentDoctorCommand(a))
	return cmd
}

type exportReceipt struct {
	RunID  int64  `json:"run_id"`
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func newAssessmentExportCommand(a *App) *cobra.Command {
	var format, file string
	var force bool
	cmd := &cobra.Command{Use: "export [RUN]", Short: "Export a stored assessment as RVTools XLSX", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.ToLower(strings.TrimSpace(format)) != "rvtools" {
			return fmt.Errorf("unsupported export format %q (supported: rvtools)", format)
		}
		if filepath.Ext(file) != ".xlsx" {
			return fmt.Errorf("--file must have a .xlsx extension")
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
		for _, warning := range exportWarnings(data) {
			fmt.Fprintf(a.errOut(), "%s %s\n", glyphFail, warning)
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return fmt.Errorf("create export directory: %w", err)
		}
		if _, err := os.Stat(file); err == nil && !force {
			return fmt.Errorf("export file %q already exists; pass --force to replace it", file)
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("check export file: %w", err)
		}
		tmp, err := os.CreateTemp(filepath.Dir(file), ".vsfleet-export-*.xlsx")
		if err != nil {
			return fmt.Errorf("create temporary export: %w", err)
		}
		tmpName := tmp.Name()
		defer os.Remove(tmpName)
		if err := tmp.Chmod(0o600); err != nil {
			_ = tmp.Close()
			return err
		}
		if err := report.WriteRVTools(tmp, data); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("write RVTools export: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return fmt.Errorf("close temporary export: %w", err)
		}
		info, err := os.Stat(tmpName)
		if err != nil {
			return err
		}
		hash, err := fileSHA256(tmpName)
		if err != nil {
			return err
		}
		if force {
			if err := os.Rename(tmpName, file); err != nil {
				return fmt.Errorf("publish export: %w", err)
			}
		} else {
			if err := os.Link(tmpName, file); err != nil {
				if os.IsExist(err) {
					return fmt.Errorf("export file %q already exists; pass --force to replace it", file)
				}
				return fmt.Errorf("publish export: %w", err)
			}
			_ = os.Remove(tmpName)
		}
		receipt := exportReceipt{RunID: data.Run.ID, Path: file, Bytes: info.Size(), SHA256: hash}
		if a.json() {
			return writeJSON(a.out(), receipt)
		}
		fmt.Fprintf(a.out(), "assessment %d exported to %s (%d bytes, sha256 %s)\n", receipt.RunID, receipt.Path, receipt.Bytes, receipt.SHA256)
		return nil
	}}
	cmd.Flags().StringVar(&format, "format", "rvtools", "export format: rvtools")
	cmd.Flags().StringVar(&file, "file", "", "destination .xlsx file")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing export")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func exportWarnings(data assessment.ExportData) []string {
	var warnings []string
	if data.Run.Status != assessment.RunComplete {
		warnings = append(warnings, fmt.Sprintf("assessment %d is %s; export coverage may be incomplete", data.Run.ID, data.Run.Status))
	}
	for _, c := range data.Contexts {
		if c.VMStatus != "success" && c.VMStatus != "empty" {
			warnings = append(warnings, fmt.Sprintf("%s VM collection: %s", c.Name, nonemptyExport(c.Error, c.VMStatus)))
		}
		seen := make(map[string]bool)
		for _, collection := range c.Collections {
			seen[collection.Kind] = true
			if collection.Status != "success" && collection.Status != "empty" {
				warnings = append(warnings, fmt.Sprintf("%s %s collection: %s", c.Name, collection.Kind, nonemptyExport(collection.Error, collection.Status)))
			}
		}
		for _, kind := range []string{"host", "cluster", "datastore"} {
			if !seen[kind] {
				warnings = append(warnings, fmt.Sprintf("%s %s collection was not recorded", c.Name, kind))
			}
		}
	}
	return warnings
}

func nonemptyExport(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func newAssessmentTrendsCommand(a *App) *cobra.Command {
	cmd := &cobra.Command{Use: "trends", Short: "Show historical estate trends"}
	cmd.AddCommand(newAssessmentChurnTrendCommand(a), newAssessmentSnapshotTrendCommand(a), newAssessmentCapacityTrendCommand(a))
	return cmd
}

type trendFlags struct {
	from, to       string
	limit          int
	includePartial bool
	contexts       []string
}

func (f *trendFlags) add(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.from, "from", "", "first assessment ID or label (inclusive)")
	cmd.Flags().StringVar(&f.to, "to", "", "last assessment ID or label (inclusive)")
	cmd.Flags().IntVar(&f.limit, "limit", 30, "number of complete assessments (0 means unlimited)")
	cmd.Flags().BoolVar(&f.includePartial, "include-partial", false, "include partial assessments")
	cmd.Flags().StringSliceVar(&f.contexts, "context", nil, "limit trend data to context(s)")
}

func (f trendFlags) options(ctx context.Context, s *assessment.Store, fallbackContexts []string) (assessment.TrendOptions, error) {
	contexts := f.contexts
	if len(contexts) == 0 {
		contexts = fallbackContexts
	}
	opts := assessment.TrendOptions{Limit: f.limit, IncludePartial: f.includePartial, Contexts: contexts}
	var err error
	if f.from != "" {
		opts.FromID, err = s.ResolveRun(ctx, f.from)
		if err != nil {
			return opts, fmt.Errorf("--from: %w", err)
		}
	}
	if f.to != "" {
		opts.ToID, err = s.ResolveRun(ctx, f.to)
		if err != nil {
			return opts, fmt.Errorf("--to: %w", err)
		}
	}
	return opts, nil
}

func newAssessmentChurnTrendCommand(a *App) *cobra.Command {
	var flags trendFlags
	cmd := &cobra.Command{Use: "churn", Short: "Show VM population and churn over time", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := a.History()
		if err != nil {
			return err
		}
		opts, err := flags.options(cmd.Context(), s, a.ContextNames)
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
	}}
	flags.add(cmd)
	return cmd
}

func newAssessmentSnapshotTrendCommand(a *App) *cobra.Command {
	var flags trendFlags
	var older string
	cmd := &cobra.Command{Use: "snapshots", Short: "Show snapshot-age trends", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := a.History()
		if err != nil {
			return err
		}
		opts, err := flags.options(cmd.Context(), s, a.ContextNames)
		if err != nil {
			return err
		}
		age, err := parseHumanDuration(older)
		if err != nil {
			return fmt.Errorf("--older-than: %w", err)
		}
		trend, err := s.SnapshotTrend(cmd.Context(), opts, age)
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
			t.row(strconv.FormatInt(p.Run.ID, 10), p.Run.StartedAt.Local().Format("2006-01-02"), itoa(p.Total), itoa(p.Stale), humanDuration(p.OldestAge), "")
		}
		t.flush()
		if len(values) > 0 {
			fmt.Fprintf(a.out(), "snapshot totals: %s\n", sparkline(values))
		}
		return nil
	}}
	flags.add(cmd)
	cmd.Flags().StringVar(&older, "older-than", "30d", "stale snapshot threshold (e.g. 30d, 2w)")
	return cmd
}

func newAssessmentCapacityTrendCommand(a *App) *cobra.Command {
	var flags trendFlags
	var kind string
	cmd := &cobra.Command{Use: "capacity", Short: "Show compute and storage capacity trends", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := a.History()
		if err != nil {
			return err
		}
		opts, err := flags.options(cmd.Context(), s, a.ContextNames)
		if err != nil {
			return err
		}
		kind = strings.ToLower(strings.TrimSpace(kind))
		if kind != "all" && kind != "host" && kind != "cluster" && kind != "datastore" {
			return fmt.Errorf("--kind must be host, cluster, datastore, or all")
		}
		kinds := []string{kind}
		if kind == "all" {
			kinds = []string{"host", "cluster", "datastore"}
		}
		trend, err := s.CapacityTrend(cmd.Context(), opts, kinds)
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
	}}
	flags.add(cmd)
	cmd.Flags().StringVar(&kind, "kind", "all", "resource kind: host, cluster, datastore, or all")
	return cmd
}

func newAssessmentReportCommand(a *App) *cobra.Command {
	var older string
	cmd := &cobra.Command{Use: "report [RUN]", Short: "Render a point-in-time assessment report", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := a.History()
		if err != nil {
			return err
		}
		selector := "latest"
		if len(args) == 1 {
			selector = args[0]
		}
		runID, err := s.ResolveRun(cmd.Context(), selector)
		if err != nil {
			return err
		}
		age, err := parseHumanDuration(older)
		if err != nil {
			return fmt.Errorf("--older-than: %w", err)
		}
		report, err := s.Report(cmd.Context(), runID, age)
		if err != nil {
			return err
		}
		if a.json() {
			return writeJSON(a.out(), report)
		}
		fmt.Fprintf(a.out(), "Assessment %d  %s  %s\n", report.Run.ID, report.Run.Status, report.Run.StartedAt.Local().Format("2006-01-02 15:04:05"))
		fmt.Fprintf(a.out(), "VMs: %d  Hosts: %d  Clusters: %d  Datastores: %d  Snapshots: %d (%d stale)\n", report.VMCount, report.HostCount, report.ClusterCount, report.DatastoreCount, report.SnapshotTotal, report.SnapshotStale)
		t := newTable(a.out(), "CONTEXT", "KIND", "STATUS", "ITEMS", "ERROR")
		for _, c := range report.Coverage {
			t.row(c.Context, c.Kind, c.Status, itoa(c.ItemCount), c.Error)
		}
		t.flush()
		for _, w := range report.Warnings {
			fmt.Fprintf(a.errOut(), "%s %s\n", glyphFail, w)
		}
		return nil
	}}
	cmd.Flags().StringVar(&older, "older-than", "30d", "stale snapshot threshold (e.g. 30d, 2w)")
	return cmd
}

func newAssessmentPruneCommand(a *App) *cobra.Command {
	var older string
	var keepLast int
	var execute bool
	cmd := &cobra.Command{Use: "prune", Short: "Prune old assessment history", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		d, err := parseHumanDuration(older)
		if err != nil || d <= 0 {
			if err == nil {
				err = fmt.Errorf("duration must be greater than zero")
			}
			return fmt.Errorf("--older-than: %w", err)
		}
		s, err := a.History()
		if err != nil {
			return err
		}
		result, err := s.Prune(cmd.Context(), d, keepLast, execute)
		if err != nil {
			return err
		}
		if a.json() {
			return writeJSON(a.out(), result)
		}
		mode := "dry-run"
		if execute {
			mode = "deleted"
		}
		fmt.Fprintf(a.out(), "assessment prune %s: %d candidate(s)\n", mode, len(result.Candidates))
		for _, c := range result.Candidates {
			fmt.Fprintf(a.out(), "  #%d %s %s (%s)\n", c.Run.ID, c.Run.Status, c.Run.StartedAt.Local().Format("2006-01-02 15:04:05"), humanBytes(c.Bytes))
		}
		if execute {
			fmt.Fprintf(a.out(), "deleted: %d\n", result.Deleted)
		}
		return nil
	}}
	cmd.Flags().StringVar(&older, "older-than", "", "required age of runs to consider (e.g. 90d)")
	cmd.Flags().IntVar(&keepLast, "keep-last", 2, "preserve this many newest completed runs")
	cmd.Flags().BoolVar(&execute, "execute", false, "perform deletion (default is a dry run)")
	return cmd
}

func newAssessmentBackupCommand(a *App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{Use: "backup FILE", Short: "Create a consistent SQLite history backup", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := a.History()
		if err != nil {
			return err
		}
		if err := s.Backup(cmd.Context(), args[0], force); err != nil {
			return err
		}
		if a.json() {
			return writeJSON(a.out(), map[string]string{"backup": args[0]})
		}
		fmt.Fprintf(a.out(), "history backup written to %s\n", args[0])
		return nil
	}}
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing backup")
	return cmd
}

func newAssessmentRestoreCommand(a *App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{Use: "restore FILE", Short: "Restore SQLite history from a backup", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := a.History()
		if err != nil {
			return err
		}
		safety, err := s.Restore(cmd.Context(), args[0], force)
		if err != nil {
			return err
		}
		if a.json() {
			return writeJSON(a.out(), map[string]string{"restored": args[0], "safety_backup": safety})
		}
		fmt.Fprintf(a.out(), "history restored from %s\npre-restore safety backup: %s\n", args[0], safety)
		return nil
	}}
	cmd.Flags().BoolVar(&force, "force", false, "confirm in-place restore")
	return cmd
}

func newAssessmentDoctorCommand(a *App) *cobra.Command {
	cmd := &cobra.Command{Use: "doctor", Short: "Check assessment database integrity", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := a.History()
		if err != nil {
			return err
		}
		report, err := s.Doctor(cmd.Context())
		if err != nil {
			return err
		}
		if a.json() {
			if err := writeJSON(a.out(), report); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(a.out(), "schema: %d  integrity: %s  foreign keys: %s\n", report.SchemaVersion, report.Integrity, report.ForeignKeys)
			fmt.Fprintf(a.out(), "runs: %d  VMs: %d  resources: %d  database: %s\n", report.Runs, report.VMObservations, report.ResourceObservations, humanBytes(report.DatabaseBytes))
			for _, warning := range report.Warnings {
				fmt.Fprintf(a.out(), "%s %s\n", glyphFail, warning)
			}
		}
		if len(report.Warnings) > 0 {
			return &doctorExitError{message: "assessment database checks reported warnings"}
		}
		return nil
	}}
	return cmd
}

func capacityCell(value *float64) string {
	if value == nil {
		return "—"
	}
	return strconv.FormatFloat(*value, 'f', 1, 64)
}

func percentCell(value *float64) string {
	if value == nil {
		return "—"
	}
	return strconv.FormatFloat(*value, 'f', 1, 64)
}

func sparkline(values []float64) string {
	if len(values) == 0 {
		return ""
	}
	glyphs := []rune("▁▂▃▄▅▆▇█")
	min, max := values[0], values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	if max == min {
		return strings.Repeat(string(glyphs[0]), len(values))
	}
	var b strings.Builder
	for _, v := range values {
		i := int((v - min) / (max - min) * float64(len(glyphs)-1))
		b.WriteRune(glyphs[i])
	}
	return b.String()
}

func newAssessmentRunCommand(a *App) *cobra.Command {
	var label, note string
	var pin bool
	cmd := &cobra.Command{Use: "run", Short: "Capture a point-in-time inventory assessment", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
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
		fmt.Fprintf(a.out(), "Appeared: %d  Vanished: %d  Moved: %d  Modified: %d  Resources: %d  Snapshots: %d\n", d.Counts.Appeared, d.Counts.Vanished, d.Counts.Moved, d.Counts.Modified, d.Counts.Resources, d.Counts.Snapshots)
		t := newTable(a.out(), "CHANGE", "RESOURCE", "CONTEXT", "DETAIL")
		for _, v := range d.VMs {
			t.row(strings.Join(v.Changes, ","), v.Name, v.Context, changeDetail(v))
		}
		for _, v := range d.Snapshots {
			t.row("snapshot "+v.Kind, v.VMName, v.Context, v.Name)
		}
		for _, r := range d.Resources {
			t.row(strings.Join(r.Changes, ","), r.Kind+"/"+r.Name, r.Context, resourceChangeDetail(r))
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
	if strings.HasPrefix(rule, "host-") || strings.HasPrefix(rule, "cluster-") || strings.HasPrefix(rule, "datastore-") {
		kind, change := strings.SplitN(rule, "-", 2)[0], strings.SplitN(rule, "-", 2)[1]
		return (kind == "host" || kind == "cluster" || kind == "datastore") && (change == "appeared" || change == "vanished" || change == "modified")
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

func resourceChangeDetail(v assessment.ResourceChange) string {
	if len(v.Fields) == 0 {
		return ""
	}
	parts := make([]string, len(v.Fields))
	for i, f := range v.Fields {
		parts[i] = f.Field + ":" + f.Before + "→" + f.After
	}
	return strings.Join(parts, " ")
}
