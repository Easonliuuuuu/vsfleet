package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/rvimport"
)

// newImportCommand groups every offline import path into assessment history.
// Import is deliberately its own top-level verb rather than an "assessment"
// subcommand: it never contacts a vCenter, and grouping it with capture would
// suggest it does.
func newImportCommand(a *App) *cobra.Command {
	cmd := requireSubcommand(&cobra.Command{
		Use:   "import",
		Short: "Import estate snapshots from other tools into assessment history",
		Long: strings.TrimSpace(`
Bring evidence vsfleet did not collect itself into the same local history used
by "vsfleet assessment", so stored-evidence commands — history, diff, trends —
work on it exactly as they would on a live capture.

Every import runs entirely offline: no configuration, credentials, or vCenter
connection is used or required.`),
		Example: `  # Import an RVTools export
  vsfleet import rvtools estate_2026-01-01.xlsx`,
	})
	cmd.AddCommand(newImportRVToolsCommand(a))
	return cmd
}

func newImportRVToolsCommand(a *App) *cobra.Command {
	var (
		label, note, capturedAt string
		contextMap              []string
		dryRun                  bool
	)
	cmd := &cobra.Command{
		Use:   "rvtools FILE",
		Short: "Import an RVTools-compatible XLSX export as a new assessment run",
		Long: strings.TrimSpace(`
Adapt an RVTools-compatible XLSX export into a new stored assessment run.

The workbook's own layout never reaches vsfleet's domain model directly: this
reads vInfo, vCPU, vMemory, vDisk, vNetwork, vHost, vCluster, vDatastore and
vSnapshot by column name and normalizes what they carry. A worksheet or
column this importer does not recognize is reported, not silently dropped,
and a field the workbook does not carry is left absent rather than defaulted
to a value that would read as confirmed evidence. RVTools has no standalone
worksheet for resource pools, distributed switches, or networks in this
profile, so those three collections are always recorded as not collected; a
workbook with no vSnapshot worksheet at all gets the same treatment for
snapshots — an explicit gap, the same way a live capture records a denied
query, never a silent one.

vCenter identity is reconstructed from the workbook's own "vsfleet Context" or
"VI SDK Server" column, never by matching display names alone: two contexts
with an identically named VM stay two VMs. --context-map renames a
reconstructed context without touching today's configuration — an imported
run's contexts are independent of config.toml.

The import is all-or-nothing: nothing is written until the whole workbook has
been read, and a failure partway through deletes the run rather than leaving
it half-written.`),
		Example: `  # Import a workbook as a new run
  vsfleet import rvtools estate.xlsx

  # Preview what would be imported without writing anything
  vsfleet import rvtools estate.xlsx --dry-run

  # Label it, pin the capture time, and rename a reconstructed context
  vsfleet import rvtools estate.xlsx --label pre-migration --captured-at 2026-01-01T00:00:00Z --context-map vc01.example.com=prod`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := rvimport.Options{SourceLabel: filepath.Base(args[0]), Label: label, Note: note}
			if strings.TrimSpace(capturedAt) != "" {
				t, err := time.Parse(time.RFC3339, capturedAt)
				if err != nil {
					return fmt.Errorf("--captured-at must be RFC3339 (e.g. 2026-01-02T15:04:05Z): %w", err)
				}
				opts.CapturedAt = t
			}
			if len(contextMap) > 0 {
				m := make(map[string]string, len(contextMap))
				for _, kv := range contextMap {
					key, value, ok := strings.Cut(kv, "=")
					key, value = strings.TrimSpace(key), strings.TrimSpace(value)
					if !ok || key == "" || value == "" {
						return fmt.Errorf("--context-map must be KEY=NAME, got %q", kv)
					}
					m[key] = value
				}
				opts.ContextMap = m
			}

			f, err := rvimport.OpenFile(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			result, err := rvimport.Parse(f, opts)
			if err != nil {
				return err
			}
			if dryRun {
				return printImportReport(a, result.Report, nil)
			}
			store, err := a.History()
			if err != nil {
				return err
			}
			run, err := result.Write(cmd.Context(), store, time.Now())
			if err != nil {
				return err
			}
			return printImportReport(a, result.Report, &run)
		},
	}
	cmd.Flags().StringVar(&label, "label", "", "label for the stored run")
	cmd.Flags().StringVar(&note, "note", "", "free-text note stored with the run, alongside this import's own provenance")
	cmd.Flags().StringVar(&capturedAt, "captured-at", "", "RFC3339 time the workbook's data was captured (default: the workbook's own metadata, else import time)")
	cmd.Flags().StringSliceVar(&contextMap, "context-map", nil, "rename a reconstructed context, KEY=NAME (repeatable); KEY is the vsfleet Context or VI SDK Server value the report names")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be imported without writing to history")
	return cmd
}

func printImportReport(a *App, report rvimport.Report, run *assessment.Run) error {
	if a.json() {
		payload := map[string]any{"report": report}
		if run != nil {
			payload["run"] = *run
		}
		return writeJSON(a.out(), payload)
	}
	out := a.out()
	if run != nil {
		fmt.Fprintf(out, "Imported assessment %d (%s)\n", run.ID, strings.ToUpper(string(run.Status)))
	} else {
		fmt.Fprintln(out, "Dry run — nothing was written")
	}
	fmt.Fprintf(out, "Captured at: %s (%s)\n", report.CapturedAt.Local().Format("2006-01-02 15:04:05"), report.CapturedAtSource)
	fmt.Fprintf(out, "Recognized worksheets: %s\n", strings.Join(report.RecognizedSheets, ", "))
	if len(report.IgnoredSheets) > 0 {
		fmt.Fprintf(out, "Ignored worksheets: %s\n", strings.Join(report.IgnoredSheets, ", "))
	}
	if len(report.SkippedKinds) > 0 {
		fmt.Fprintf(out, "Not collected by this profile: %s\n", strings.Join(report.SkippedKinds, ", "))
	}
	fmt.Fprintln(out)
	t := newTable(out, "CONTEXT", "ENDPOINT", "VMS", "HOSTS", "CLUSTERS", "DATASTORES", "SNAPSHOTS")
	for _, c := range report.Contexts {
		t.row(c.Name, dash(c.Endpoint), itoa(c.VMCount), itoa(c.HostCount), itoa(c.ClusterCount), itoa(c.DatastoreCount), itoa(c.SnapshotCount))
	}
	t.flush()
	if len(report.Warnings) > 0 {
		fmt.Fprintln(a.errOut())
		for _, w := range report.Warnings {
			fmt.Fprintf(a.errOut(), "%s %s\n", glyphFail, w)
		}
	}
	return nil
}
