package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/apiresource"
	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/version"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// newCaptureCommand groups everything that adds, changes or removes a stored
// capture. Inside the group a capture is the object acted on, so it is a
// positional argument ("capture delete 42"); everywhere else --run picks one.
func newCaptureCommand(a *App) *cobra.Command {
	cmd := requireSubcommand(&cobra.Command{
		Use:   "capture",
		Short: "Take, import, label, compare and remove stored captures",
		Long: strings.TrimSpace(`
A capture is a point-in-time snapshot of the estate, stored in the local
history database. "capture run" takes one from vCenter and "capture import"
adds one from an RVTools export; the other subcommands list, label, compare
and remove them.

Every other command reads a capture with --run: "vsfleet get vms --run
latest", "vsfleet describe capture latest", "vsfleet analyze health". RUN is an
ID or label from "capture list", latest, previous, or a date (2026-03-31, or
2026-03-31T18:00) for the newest capture started by then.

Performance collections ("capture perf") are stored apart from captures:
"capture list", "capture delete" and --run do not see them, and "vsfleet get
perfcollections" lists them. "capture prune" removes old ones along with old
captures.

Only "capture run" and "capture perf" contact a vCenter.`),
		Example: `  # Capture every context and label the capture
  vsfleet capture run -A --label nightly

  # See what has been captured, then compare the two most recent
  vsfleet capture list
  vsfleet capture diff

  # Compare a labelled baseline against the latest capture
  vsfleet capture diff nightly latest

  # Fail a pipeline when VMs were removed
  vsfleet capture diff --fail-on removed

  # Read a capture without touching a vCenter
  vsfleet describe capture latest
  vsfleet get vms --run nightly -A`,
	})
	cmd.AddCommand(newCaptureRunCommand(a), newCaptureImportCommand(a), newCaptureListCommand(a), newCaptureUpdateCommand(a),
		newCaptureDeleteCommand(a), newCapturePruneCommand(a), newCaptureDiffCommand(a), newCapturePerfCommand(a))
	return cmd
}

// newCaptureImportCommand groups the offline import paths. Each one adds a
// capture without contacting a vCenter.
func newCaptureImportCommand(a *App) *cobra.Command {
	cmd := requireSubcommand(&cobra.Command{
		Use:   "import",
		Short: "Add a capture from another tool's export",
		Long: strings.TrimSpace(`
Bring evidence vsfleet did not collect itself into the history database as a
capture, so --run, "capture diff" and the analyses work on it exactly as they
would on a live one.

Every import runs entirely offline: no configuration, credentials, or vCenter
connection is used or required.`),
		Example: `  # Import an RVTools export
  vsfleet capture import rvtools estate_2026-01-01.xlsx`,
	})
	cmd.AddCommand(newImportRVToolsCommand(a))
	return cmd
}

// newCaptureListCommand is "get captures" under the name the group gives it.
func newCaptureListCommand(a *App) *cobra.Command {
	var noHeaders bool
	cmd := &cobra.Command{Use: "list", Aliases: []string{"ls"}, Short: "List stored captures", Long: strings.TrimSpace(`
List the captures in the local history database, newest first, the same as
"vsfleet get captures".

The ID and label are what --run and the other capture subcommands accept.
Performance collections are not captures; "vsfleet get perfcollections" lists
them.`), Example: `  # Every stored capture, newest first
  vsfleet capture list

  # Only the IDs and labels, to pick one in a script
  vsfleet capture list -o custom-columns=ID:.id,LABEL:.label --no-headers`,
		Annotations: map[string]string{annotationAllOutputs: "true"},
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rejectStoredContextSelection(cmd, "list"); err != nil {
				return err
			}
			spec, err := parseOutput(a.Format)
			if err != nil {
				return err
			}
			opt, err := getFlags{}.options(spec)
			if err != nil {
				return err
			}
			reqs := []resourceRequest{{kind: apiresource.Get("capture")}}
			return getRun{a: a, reqs: reqs, opt: opt, spec: spec, noHeaders: noHeaders}.once(cmd.Context())
		}}
	cmd.Flags().BoolVar(&noHeaders, "no-headers", false, "leave out the header row of table, wide, csv and custom-columns output")
	return cmd
}

func newCapturePruneCommand(a *App) *cobra.Command {
	var older string
	var keepLast int
	var execute bool
	cmd := &cobra.Command{Use: "prune", Short: "Remove old captures and performance collections", Long: strings.TrimSpace(`
Delete captures older than --older-than from the local history database,
and the performance collections older than it too, keeping the newest
collection per context.

Pruning is a dry run unless --execute is passed, so the default output is a
preview of what would go. Pinned captures are never pruned.`), Example: `  # Preview what a prune would remove (nothing is deleted)
  vsfleet capture prune --older-than 90d

  # Actually remove them, keeping the 5 newest complete captures
  vsfleet capture prune --older-than 90d --keep-last 5 --execute`, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := rejectStoredContextSelection(cmd, "prune"); err != nil {
			return err
		}
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
		fmt.Fprintf(a.out(), "capture prune %s: %d candidate(s)\n", mode, len(result.Candidates))
		for _, c := range result.Candidates {
			fmt.Fprintf(a.out(), "  #%d %s %s (%s)\n", c.Run.ID, c.Run.Status, c.Run.StartedAt.Local().Format("2006-01-02 15:04:05"), humanBytes(c.Bytes))
		}
		if result.PerfWindows > 0 {
			fmt.Fprintf(a.out(), "performance windows older than the cutoff: %d (the newest per context is kept)\n", result.PerfWindows)
		}
		if execute {
			fmt.Fprintf(a.out(), "deleted: %d\n", result.Deleted)
			if result.PerfWindows > 0 {
				fmt.Fprintf(a.out(), "performance windows deleted: %d\n", result.PerfDeleted)
			}
		}
		return nil
	}}
	cmd.Flags().StringVar(&older, "older-than", "", "required age of runs to consider (e.g. 90d)")
	cmd.Flags().IntVar(&keepLast, "keep-last", 2, "preserve this many newest completed runs")
	cmd.Flags().BoolVar(&execute, "execute", false, "perform deletion (default is a dry run)")
	return cmd
}

func newCaptureRunCommand(a *App) *cobra.Command {
	var label, note string
	var pin bool
	var browseDatastores, includeLicenses bool
	var fileInventory bool
	var inventoryMaxFiles, inventoryMaxTotal int
	var inventoryTimeout time.Duration
	var failOnPartial bool
	cmd := &cobra.Command{Use: "run", Short: "Take a capture of the estate from vCenter", Long: strings.TrimSpace(`
Read inventory from the selected contexts and store it as one immutable
capture in the local history database.

-n picks the contexts and -A captures every one. Apart from "capture perf",
this is the only command that writes vCenter evidence to the history
database; everything that reads a capture reads back what it stored. A
capture that reaches some contexts but not all is stored as partial and says
so.`), Example: `  # Capture the current context
  vsfleet capture run

  # Capture the whole estate under a stable label
  vsfleet capture run --all-contexts --label nightly

  # Also record datastore VMDK files, so "analyze orphans" has evidence
  vsfleet capture run --all-contexts --browse-datastores

  # Also record license products, usage, expiration and host assignments for a
  # licensing review (keys are never stored or exported)
  vsfleet capture run --all-contexts --include-licenses

  # Also list every datastore file, for the optional vFileInfo export tab.
  # Opt-in and bounded; exports filenames and paths (see docs/assessments.md)
  vsfleet capture run --all-contexts --datastore-file-inventory

  # Pin a baseline so pruning never removes it
  vsfleet capture run --all-contexts --label pre-migration --pin

  # Gate a scheduled job: exit 3 when a site was unreachable
  vsfleet capture run --all-contexts --fail-on-partial`, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		inventoryOptions, err := fileInventoryOptions(cmd, fileInventory, inventoryMaxFiles, inventoryMaxTotal, inventoryTimeout)
		if err != nil {
			return err
		}
		contexts, err := a.Contexts()
		if err != nil {
			return err
		}
		service, err := a.Assessment()
		if err != nil {
			return err
		}
		run, err := service.Capture(cmd.Context(), assessment.CaptureOptions{Contexts: contexts, Source: "cli", Label: label, Note: note, Pinned: pin, BrowseDatastores: browseDatastores, IncludeLicenses: includeLicenses, FileInventory: inventoryOptions, ToolVersion: version.String(), InventorySchemaVersion: assessment.CurrentInventorySchemaVersion, Progress: func(p assessment.ContextProgress) {
			if p.Error != nil {
				fmt.Fprintf(a.errOut(), "%s %s: %v\n", glyphFail, p.Context, p.Error)
			}
			for _, collection := range p.Collections {
				if collection.Kind == assessment.LicenseKind && collection.Status != "success" && collection.Status != "empty" {
					fmt.Fprintf(a.errOut(), "%s %s license collection %s: %v\n", glyphFail, p.Context, collection.Status, collection.Error)
				}
			}
		}})
		if err != nil {
			return err
		}
		var coverage []assessment.FileInventoryContext
		var browse assessment.BrowseSummary
		if inventoryOptions != nil || browseDatastores {
			// Read the stored evidence back rather than trusting in-memory
			// state, so what is reported is exactly what an export will say.
			if store, storeErr := a.History(); storeErr == nil {
				if data, loadErr := store.LoadExportData(cmd.Context(), run.ID); loadErr == nil {
					if inventoryOptions != nil {
						coverage = assessment.FileInventoryCoverage(data)
					}
					if browseDatastores {
						browse = assessment.SummarizeBrowse(data)
					}
				} else {
					fmt.Fprintf(a.errOut(), "%s capture coverage could not be read back: %v\n", glyphFail, loadErr)
				}
			}
		}
		if a.json() {
			payload := struct {
				assessment.Run
				DatastoreBrowse *assessment.BrowseSummary         `json:"datastore_browse,omitempty"`
				FileInventory   []assessment.FileInventoryContext `json:"file_inventory,omitempty"`
			}{Run: run, FileInventory: coverage}
			if browse.Requested {
				payload.DatastoreBrowse = &browse
			}
			if writeErr := writeJSON(a.out(), payload); writeErr != nil {
				return writeErr
			}
		} else {
			printRun(a.out(), run, browse.Note())
			printFileInventoryCoverage(a.out(), coverage)
		}
		for _, warning := range fileInventoryWarnings(coverage) {
			fmt.Fprintf(a.errOut(), "%s %s\n", glyphFail, warning)
		}
		if run.Status == assessment.RunFailed {
			return fmt.Errorf("capture %d failed: no context returned VM inventory", run.ID)
		}
		if failOnPartial && run.Status == assessment.RunPartial {
			return &partialExitError{runID: run.ID, successful: run.SuccessfulContexts, total: run.RequestedContexts}
		}
		return nil
	}}
	cmd.Flags().StringVar(&label, "label", "", "stable label for this capture, which --run and RUN accept")
	cmd.Flags().StringVar(&note, "note", "", "operator note stored with this capture")
	cmd.Flags().BoolVar(&pin, "pin", false, "pin this capture so prune never removes it")
	cmd.Flags().BoolVar(&browseDatastores, "browse-datastores", false, "record VM disk files from each accessible datastore (requires Datastore.Browse; adds time on large estates)")
	cmd.Flags().BoolVar(&includeLicenses, "include-licenses", false, "record license products, usage, expiration and host assignments (read-only; needs the Global.Licenses privilege; license keys are never stored)")
	cmd.Flags().BoolVar(&fileInventory, "datastore-file-inventory", false, "also list every file on each accessible datastore for the optional vFileInfo export tab (opt-in; needs Datastore.Browse; exports filenames and paths; bounded by the --file-inventory-* limits; independent of --browse-datastores)")
	cmd.Flags().IntVar(&inventoryMaxFiles, "file-inventory-max-files", vsphere.DefaultFileInventoryMaxFiles, "with --datastore-file-inventory: most file rows kept per datastore; a datastore that hits it is recorded as truncated, never complete")
	cmd.Flags().IntVar(&inventoryMaxTotal, "file-inventory-max-total-files", vsphere.DefaultFileInventoryMaxTotalFiles, "with --datastore-file-inventory: most file rows kept per context; datastores not reached are recorded as skipped")
	cmd.Flags().DurationVar(&inventoryTimeout, "file-inventory-timeout", vsphere.DefaultFileInventoryDatastoreTimeout, "with --datastore-file-inventory: time allowed to list one datastore before it is recorded as failed (each context also gets three times this, at most)")
	cmd.Flags().BoolVar(&failOnPartial, "fail-on-partial", false, "exit 3 when the capture stored evidence from some contexts but not all")
	return cmd
}

func newCaptureDiffCommand(a *App) *cobra.Command {
	var runtime bool
	var failOn []string
	var maxAge string
	var requireComplete bool
	cmd := &cobra.Command{Use: "diff [BASE] [TARGET]", Short: "Compare two captures", Long: strings.TrimSpace(`
Compare two stored captures and report what changed between them.

BASE and TARGET are IDs or labels from "vsfleet capture list", latest,
previous, or a date, the way --run takes them. With no arguments the two most
recent captures are compared; with one, that capture is compared against the
one before it.

Volatile runtime fields — power state, guest, IP, tools, storage — are left
out unless --include-runtime is passed, so ordinary churn does not drown the
structural changes.`), Example: `  # The two most recent captures
  vsfleet capture diff

  # A labelled baseline against the most recent capture
  vsfleet capture diff pre-migration latest

  # Two captures by ID, including volatile runtime fields
  vsfleet capture diff 41 42 --include-runtime

  # Gate a pipeline: exit 2 when VMs were removed or networks changed
  vsfleet capture diff --fail-on removed,network-changed

  # Refuse to report on captures that are not fully comparable
  vsfleet capture diff --require-complete`, Args: cobra.MaximumNArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := a.History()
		if err != nil {
			return err
		}
		base, target, err := resolveRunPair(cmd.Context(), s, args)
		if err != nil {
			return err
		}
		d, err := s.DiffForContexts(cmd.Context(), base, target, runtime, a.StoredContextNames())
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
		fmt.Fprintf(a.out(), "Capture %d → %d  (%s → %s)\n", d.Base.ID, d.Target.ID, d.Base.Status, d.Target.Status)
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

func newCaptureDeleteCommand(a *App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{Use: "delete RUN", Short: "Delete one stored capture", Example: `  # Delete one capture by ID (deletion must be confirmed)
  vsfleet capture delete 42 --force

  # Delete a labelled capture
  vsfleet capture delete nightly --force`, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := rejectStoredContextSelection(cmd, "delete"); err != nil {
			return err
		}
		if !force {
			return fmt.Errorf("deleting a capture requires --force")
		}
		s, err := a.History()
		if err != nil {
			return err
		}
		id, err := s.ResolveRun(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		run, err := s.GetRun(cmd.Context(), id)
		if err != nil {
			return err
		}
		if err := s.DeleteRun(cmd.Context(), id); err != nil {
			return err
		}
		if a.json() {
			return writeJSON(a.out(), map[string]assessment.Run{"deleted": run})
		}
		fmt.Fprintf(a.out(), "deleted capture %s\n", args[0])
		return nil
	}}
	cmd.Flags().BoolVar(&force, "force", false, "confirm deletion")
	return cmd
}

func newCaptureUpdateCommand(a *App) *cobra.Command {
	var label, note string
	var pin, unpin bool
	cmd := &cobra.Command{Use: "update RUN", Short: "Set a capture's label, note or pin", Example: `  # Label a capture so other commands can select it by name
  vsfleet capture update 42 --label pre-migration

  # Pin it against pruning, with a note
  vsfleet capture update 42 --pin --note "baseline for the Q3 migration"

  # Clear the note and unpin
  vsfleet capture update 42 --note "" --unpin`, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := rejectStoredContextSelection(cmd, "update"); err != nil {
			return err
		}
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

// resolveRunPair picks the captures "capture diff" compares: the two most
// recent with no arguments, the one named and the capture before it with
// one, and the two named with two. A capture is selected the way --run
// selects one, so a date works here too.
func resolveRunPair(ctx context.Context, s *assessment.Store, args []string) (int64, int64, error) {
	runs, err := s.Runs(ctx)
	if err != nil {
		return 0, 0, err
	}
	if len(runs) < 2 {
		return 0, 0, fmt.Errorf("need at least two captures to compare")
	}
	if len(args) == 0 {
		return runs[1].ID, runs[0].ID, nil
	}
	ids := make([]int64, len(args))
	for i, arg := range args {
		if ids[i], err = s.ResolveRun(ctx, arg); err != nil {
			return 0, 0, err
		}
	}
	if len(args) == 2 {
		return ids[0], ids[1], nil
	}
	target := ids[0]
	for _, r := range runs {
		if r.ID != target && r.StartedAt.Before(targetTime(runs, target)) {
			return r.ID, target, nil
		}
	}
	return 0, 0, fmt.Errorf("no capture before %s to compare it with", args[0])
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

func printRun(out interface{ Write([]byte) (int, error) }, r assessment.Run, notes ...string) {
	label := ""
	if r.Label != "" {
		label = " label=" + r.Label
	}
	if r.Pinned {
		label += " pinned"
	}
	detail := fmt.Sprintf("%d/%d contexts successful", r.SuccessfulContexts, r.RequestedContexts)
	for _, note := range notes {
		if note != "" {
			detail += ", " + note
		}
	}
	fmt.Fprintf(out, "capture %d: %s%s (%s)\n", r.ID, r.Status, label, detail)
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
