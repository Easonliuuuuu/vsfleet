package cli

import (
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/health"
	"github.com/easonliuuuuu/vsfleet/internal/report"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

type policyExitError struct{ violations []assessment.PolicyViolation }

func (e *policyExitError) Error() string {
	return fmt.Sprintf("assessment diff policy failed (%d violation(s))", len(e.violations))
}
func (e *policyExitError) ExitCode() int { return 2 }

type doctorExitError struct{ message string }

func (e *doctorExitError) Error() string { return e.message }
func (e *doctorExitError) ExitCode() int { return 1 }

// partialExitError reports a capture that stored real evidence from some
// vCenters but not all of them. It is raised only under --fail-on-partial:
// a scheduled job that has always treated one site being down as routine must
// keep behaving the way it did before the flag existed.
type partialExitError struct {
	runID      int64
	successful int
	total      int
}

func (e *partialExitError) Error() string {
	return fmt.Sprintf("assessment %d is partial: %d of %d contexts returned inventory", e.runID, e.successful, e.total)
}
func (e *partialExitError) ExitCode() int { return 3 }

func newAssessmentCommand(a *App) *cobra.Command {
	cmd := requireSubcommand(&cobra.Command{
		Use:     "assessment",
		Aliases: []string{"assess", "history"},
		Short:   "Export, back up and check stored captures",
		Long: strings.TrimSpace(`
Export a stored capture as an RVTools workbook or a metadata report, and
back up, restore or check the local history database.

Taking, listing, comparing and removing captures is "vsfleet capture". Every
subcommand here reads stored evidence only and never contacts a vCenter.`),
		Example: `  # Export the most recent capture as an RVTools workbook
  vsfleet assessment export --file estate.xlsx

  # Back the history database up, then check it
  vsfleet assessment backup history.db.bak
  vsfleet assessment doctor`,
	})
	cmd.AddCommand(newAssessmentExportCommand(a), newAssessmentMetadataCommand(a), newAssessmentMetadataReportCommand(a), newAssessmentBackupCommand(a), newAssessmentRestoreCommand(a), newAssessmentDoctorCommand(a))
	for _, r := range []struct{ name, hint string }{
		{"run", `use "vsfleet capture run"`},
		{"list", `use "vsfleet capture list" or "vsfleet get captures"`},
		{"diff", `use "vsfleet capture diff"`},
		{"delete", `use "vsfleet capture delete RUN"`},
		{"update", `use "vsfleet capture update RUN"`},
		{"prune", `use "vsfleet capture prune"`},
		{"report", `use "vsfleet describe capture RUN", e.g. "vsfleet describe capture latest"`},
		{"findings", `use "vsfleet analyze health"`},
		{"readiness", `use "vsfleet analyze readiness"`},
		{"network-readiness", `use "vsfleet analyze readiness network cl/SOURCE cl/TARGET"`},
		{"sizing", `use "vsfleet analyze sizing"`},
		{"capacity", `use "vsfleet analyze capacity"`},
		{"orphans", `use "vsfleet analyze orphans"`},
		{"snapshots", `use "vsfleet analyze snapshots"`},
	} {
		cmd.AddCommand(renamedCommand(r.name, nil, r.hint))
	}
	cmd.AddCommand(renamedTrendsCommand())
	cmd.AddCommand(renamedPerfCommand())
	cmd.AddCommand(renamedCommand("inventory", nil, `use "vsfleet get KIND --run RUN", e.g. "vsfleet get vms,hosts --run latest -A"`))
	return cmd
}

type exportFile struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type exportReceipt struct {
	RunID  int64        `json:"run_id"`
	Path   string       `json:"path"`
	Bytes  int64        `json:"bytes,omitempty"`
	SHA256 string       `json:"sha256,omitempty"`
	Files  []exportFile `json:"files,omitempty"`
	// Profile, Pseudonymized, Partial and Warnings describe a scoped sharing
	// export; they are absent from the ordinary export receipt.
	Profile       string   `json:"profile,omitempty"`
	Pseudonymized bool     `json:"pseudonymized,omitempty"`
	Partial       bool     `json:"partial,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
	// FileInventory reports, per context and datastore, how complete the
	// vFileInfo rows are. It is present only when the run opted into the
	// datastore file inventory; a status other than "complete" means the
	// datastore's rows are a partial list.
	FileInventory []assessment.FileInventoryContext `json:"file_inventory,omitempty"`
}

func newAssessmentExportCommand(a *App) *cobra.Command {
	var format, file string
	var force, includeMetadata bool
	var share shareFlags
	cmd := &cobra.Command{Use: "export [RUN]", Short: "Export a stored assessment as RVTools XLSX or CSV", Example: `  # 25-sheet RVTools workbook from the most recent capture
  vsfleet assessment export --file estate.xlsx

  # One CSV per RVTools tab, into a directory
  vsfleet assessment export --format csv --file ./estate-csv

  # Export a labelled capture, replacing an existing file
  vsfleet assessment export nightly --file estate.xlsx --force

  # See what the workbook will contain before exporting
  vsfleet explain rvtools

  # Preview a scoped sizing summary, then write it with pseudonymized names
  vsfleet assessment export --profile sizing-summary --pseudonymize --preview
  vsfleet assessment export --profile sizing-summary --pseudonymize \
    --pseudonymize-key-file ~/.config/vsfleet/share.key --file sizing.xlsx`, Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		format = strings.ToLower(strings.TrimSpace(format))
		if err := share.validate(format); err != nil {
			return err
		}
		var shareKey []byte
		if share.enabled() && !share.preview && share.pseudonymize {
			key, err := readShareKey(share.keyFile, a.in())
			if err != nil {
				return err
			}
			shareKey = key
		}
		if file == "" && !share.preview {
			return fmt.Errorf("--file is required")
		}
		switch format {
		case "rvtools":
			if file != "" && filepath.Ext(file) != ".xlsx" {
				return fmt.Errorf("--file must have a .xlsx extension")
			}
		case "csv":
			if filepath.Ext(file) == ".xlsx" {
				return fmt.Errorf("--file must be a directory for --format csv, not a .xlsx path")
			}
		default:
			return fmt.Errorf("unsupported export format %q (supported: rvtools, csv)", format)
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
		data, err := s.LoadExportDataForContexts(cmd.Context(), runID, a.StoredContextNames())
		if err != nil {
			return err
		}
		healthReport := health.Evaluate(data, health.Options{Thresholds: health.DefaultThresholds()})
		for _, warning := range exportWarnings(data) {
			fmt.Fprintf(a.errOut(), "%s %s\n", glyphFail, warning)
		}
		var inventory []assessment.FileInventoryContext
		if assessment.HasFileInventory(data) {
			inventory = assessment.FileInventoryCoverage(data)
			for _, warning := range fileInventoryWarnings(inventory) {
				fmt.Fprintf(a.errOut(), "%s %s\n", glyphFail, warning)
			}
			files := 0
			for _, c := range inventory {
				files += c.Files()
			}
			fmt.Fprintf(a.errOut(), "note: vFileInfo lists %d datastore file names and paths; the export can reveal VM names and internal layout, so handle it as sensitive\n", files)
		}
		var receipt exportReceipt
		if share.enabled() {
			opts := share.options(shareKey)
			opts.Metadata = includeMetadata
			plan, err := report.PlanShare(data, healthReport, opts)
			if err != nil {
				return err
			}
			if share.preview {
				return printSharePlan(a, plan)
			}
			for _, w := range plan.Warnings {
				fmt.Fprintf(a.errOut(), "note: %s\n", w)
			}
			receipt, err = publishXLSX(data.Run.ID, file, force, func(w io.Writer) error {
				if err := report.WriteShared(w, data, healthReport, opts); err != nil {
					return fmt.Errorf("write %s export: %w", opts.Profile, err)
				}
				return nil
			})
			if err != nil {
				return err
			}
			receipt.Profile, receipt.Pseudonymized, receipt.Partial, receipt.Warnings = plan.Profile, plan.Pseudonymize, plan.Partial, plan.Warnings
			if a.json() {
				return writeJSON(a.out(), receipt)
			}
			fmt.Fprintf(a.out(), "assessment %d exported to %s with profile %s (%d bytes, sha256 %s)\n", receipt.RunID, receipt.Path, plan.Profile, receipt.Bytes, receipt.SHA256)
			return nil
		}
		exportOpts := report.ExportOptions{Metadata: includeMetadata}
		if format == "csv" {
			receipt, err = publishRVToolsCSV(data, healthReport, exportOpts, file, force)
		} else {
			receipt, err = publishRVToolsXLSX(data, healthReport, exportOpts, file, force)
		}
		if err != nil {
			return err
		}
		receipt.FileInventory = inventory
		if a.json() {
			return writeJSON(a.out(), receipt)
		}
		if format == "csv" {
			fmt.Fprintf(a.out(), "assessment %d exported to %s (%d files)\n", receipt.RunID, receipt.Path, len(receipt.Files))
			for _, f := range receipt.Files {
				fmt.Fprintf(a.out(), "  %s (%d bytes, sha256 %s)\n", f.Name, f.Bytes, f.SHA256)
			}
		} else {
			fmt.Fprintf(a.out(), "assessment %d exported to %s (%d bytes, sha256 %s)\n", receipt.RunID, receipt.Path, receipt.Bytes, receipt.SHA256)
		}
		return nil
	}}
	cmd.Flags().StringVar(&format, "format", "rvtools", "export format: rvtools (XLSX) or csv (one file per RVTools tab)")
	cmd.Flags().StringVar(&file, "file", "", "destination: a .xlsx file for --format rvtools, a directory for --format csv")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing export")
	cmd.Flags().BoolVar(&includeMetadata, "include-metadata", false, "add the vsfleetMetadata sheet: one row per tag or custom attribute value (omitted by --profile sizing-summary)")
	share.add(cmd)
	return cmd
}

// publishRVToolsXLSX renders data as one RVTools workbook at file: written to
// a sibling temp file first, then published atomically (a rename with
// --force, otherwise a no-clobber hard link) so a failed export never leaves
// a half-written workbook.
func publishRVToolsXLSX(data assessment.ExportData, healthReport health.Report, opts report.ExportOptions, file string, force bool) (exportReceipt, error) {
	return publishXLSX(data.Run.ID, file, force, func(w io.Writer) error {
		if err := report.WriteRVToolsWith(w, data, healthReport, opts); err != nil {
			return fmt.Errorf("write RVTools export: %w", err)
		}
		return nil
	})
}

// publishXLSX publishes the workbook write produces at file with the atomic,
// no-clobber-without-force behavior shared by every XLSX export.
func publishXLSX(runID int64, file string, force bool, write func(io.Writer) error) (exportReceipt, error) {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return exportReceipt{}, fmt.Errorf("create export directory: %w", err)
	}
	if _, err := os.Stat(file); err == nil && !force {
		return exportReceipt{}, fmt.Errorf("export file %q already exists; pass --force to replace it", file)
	} else if err != nil && !os.IsNotExist(err) {
		return exportReceipt{}, fmt.Errorf("check export file: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".vsfleet-export-*.xlsx")
	if err != nil {
		return exportReceipt{}, fmt.Errorf("create temporary export: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return exportReceipt{}, err
	}
	if err := write(tmp); err != nil {
		_ = tmp.Close()
		return exportReceipt{}, err
	}
	if err := tmp.Close(); err != nil {
		return exportReceipt{}, fmt.Errorf("close temporary export: %w", err)
	}
	info, err := os.Stat(tmpName)
	if err != nil {
		return exportReceipt{}, err
	}
	hash, err := fileSHA256(tmpName)
	if err != nil {
		return exportReceipt{}, err
	}
	if err := publishExportFile(tmpName, file, force); err != nil {
		return exportReceipt{}, err
	}
	return exportReceipt{RunID: runID, Path: file, Bytes: info.Size(), SHA256: hash}, nil
}

// publishRVToolsCSV renders data as one CSV file per RVTools tab under dir.
// Rendering happens entirely in memory before any file is touched, so a data
// error leaves the destination untouched; each tab is then published with
// the same atomic-publish, no-clobber-without-force behavior as the XLSX
// writer.
func publishRVToolsCSV(data assessment.ExportData, healthReport health.Report, opts report.ExportOptions, dir string, force bool) (exportReceipt, error) {
	files, err := report.RVToolsCSVWith(data, healthReport, opts)
	if err != nil {
		return exportReceipt{}, fmt.Errorf("render CSV export: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return exportReceipt{}, fmt.Errorf("create export directory: %w", err)
	}
	if !force {
		for _, file := range files {
			dest := filepath.Join(dir, file.Name)
			if _, err := os.Stat(dest); err == nil {
				return exportReceipt{}, fmt.Errorf("export file %q already exists; pass --force to replace it", dest)
			} else if !os.IsNotExist(err) {
				return exportReceipt{}, fmt.Errorf("check export file: %w", err)
			}
		}
	}
	receipt := exportReceipt{RunID: data.Run.ID, Path: dir}
	for _, file := range files {
		dest := filepath.Join(dir, file.Name)
		tmp, err := os.CreateTemp(dir, ".vsfleet-export-*.csv")
		if err != nil {
			return exportReceipt{}, fmt.Errorf("create temporary export: %w", err)
		}
		tmpName := tmp.Name()
		if err := tmp.Chmod(0o600); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
			return exportReceipt{}, err
		}
		if _, err := tmp.Write(file.Data); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
			return exportReceipt{}, fmt.Errorf("write %s: %w", file.Name, err)
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpName)
			return exportReceipt{}, fmt.Errorf("close temporary export: %w", err)
		}
		if err := publishExportFile(tmpName, dest, force); err != nil {
			return exportReceipt{}, err
		}
		receipt.Files = append(receipt.Files, exportFile{Name: file.Name, Bytes: int64(len(file.Data)), SHA256: fmt.Sprintf("%x", sha256.Sum256(file.Data))})
	}
	return receipt, nil
}

// publishExportFile atomically moves tmpName to dest: a rename when force
// replaces an existing export, or a no-clobber hard link otherwise.
func publishExportFile(tmpName, dest string, force bool) error {
	if force {
		if err := os.Rename(tmpName, dest); err != nil {
			return fmt.Errorf("publish export: %w", err)
		}
		return nil
	}
	if err := os.Link(tmpName, dest); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("export file %q already exists; pass --force to replace it", dest)
		}
		return fmt.Errorf("publish export: %w", err)
	}
	return os.Remove(tmpName)
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

func newAssessmentBackupCommand(a *App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{Use: "backup FILE", Short: "Create a consistent SQLite history backup", Example: `  # Back the history database up to a file
  vsfleet assessment backup ./vsfleet-history.db

  # Overwrite an existing backup, e.g. from a nightly job
  vsfleet assessment backup ./vsfleet-history.db --force`, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := rejectStoredContextSelection(cmd, "backup"); err != nil {
			return err
		}
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
	cmd := &cobra.Command{Use: "restore FILE", Short: "Restore SQLite history from a backup", Example: `  # Restore into a history database that holds no history yet
  vsfleet assessment restore ./vsfleet-history.db

  # Replace existing history in place (destructive, so it must be confirmed)
  vsfleet assessment restore ./vsfleet-history.db --force`, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := rejectStoredContextSelection(cmd, "restore"); err != nil {
			return err
		}
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
	cmd.Flags().BoolVar(&force, "force", false, "confirm replacing existing history")
	return cmd
}

func newAssessmentDoctorCommand(a *App) *cobra.Command {
	cmd := &cobra.Command{Use: "doctor", Short: "Check assessment database integrity", Example: `  # Check the local history database
  vsfleet assessment doctor

  # Check a specific database, as JSON
  vsfleet assessment doctor --history-db ./vsfleet-history.db -o json`, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := rejectStoredContextSelection(cmd, "doctor"); err != nil {
			return err
		}
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

var durationToken = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)([smhdw])`)

var byteToken = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*(b|k|kb|ki|kib|m|mb|mi|mib|g|gb|gi|gib|t|tb|ti|tib|p|pb|pi|pib)?$`)

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

// validatePercent rejects a flag value that is not a finite percentage in
// [0, 100], including NaN and +/-Inf, which ordinary range comparisons let
// through silently. flag is the flag's own name (e.g. "--min-free") for the
// error message.
func validatePercent(flag string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
		return fmt.Errorf("%s must be between 0 and 100", flag)
	}
	return nil
}

func parseHumanBytes(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	matches := byteToken.FindStringSubmatch(value)
	if len(matches) == 0 {
		return 0, fmt.Errorf("invalid byte size %q", value)
	}
	n, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, err
	}
	power := 0
	switch strings.ToLower(matches[2]) {
	case "", "b":
		power = 0
	case "k", "kb", "ki", "kib":
		power = 1
	case "m", "mb", "mi", "mib":
		power = 2
	case "g", "gb", "gi", "gib":
		power = 3
	case "t", "tb", "ti", "tib":
		power = 4
	case "p", "pb", "pi", "pib":
		power = 5
	}
	for i := 0; i < power; i++ {
		n *= 1024
	}
	return n, nil
}

// fileInventoryOptions turns the opt-in flag and its limits into capture
// options. It returns nil, which disables the inventory entirely, unless
// --datastore-file-inventory was given: a limit flag on its own is an error,
// never a quiet way to switch datastore browsing on.
func fileInventoryOptions(cmd *cobra.Command, enabled bool, maxFiles, maxTotal int, timeout time.Duration) (*vsphere.FileInventoryOptions, error) {
	limitFlags := []string{"file-inventory-max-files", "file-inventory-max-total-files", "file-inventory-timeout"}
	if !enabled {
		for _, name := range limitFlags {
			if cmd.Flags().Changed(name) {
				return nil, fmt.Errorf("--%s has no effect without --datastore-file-inventory; datastore file inventory is opt-in", name)
			}
		}
		return nil, nil
	}
	switch {
	case maxFiles < 1 || maxFiles > vsphere.MaxFileInventoryMaxFiles:
		return nil, fmt.Errorf("--file-inventory-max-files must be between 1 and %d", vsphere.MaxFileInventoryMaxFiles)
	case maxTotal < 1 || maxTotal > vsphere.MaxFileInventoryMaxTotalFiles:
		return nil, fmt.Errorf("--file-inventory-max-total-files must be between 1 and %d", vsphere.MaxFileInventoryMaxTotalFiles)
	case timeout < time.Second:
		return nil, fmt.Errorf("--file-inventory-timeout must be at least 1s")
	}
	return &vsphere.FileInventoryOptions{
		MaxFilesPerDatastore: maxFiles,
		MaxFilesPerContext:   maxTotal,
		DatastoreTimeout:     timeout,
		ContextTimeout:       3 * timeout,
	}, nil
}

// printFileInventoryCoverage prints one line per context and one per
// datastore that is not complete.
func printFileInventoryCoverage(out io.Writer, coverage []assessment.FileInventoryContext) {
	for _, c := range coverage {
		if !c.Requested {
			continue
		}
		status, message := c.Summary()
		fmt.Fprintf(out, "  file inventory %s: %s, %d files", c.Context, status, c.Files())
		if message != "" {
			fmt.Fprintf(out, " (%s)", message)
		}
		fmt.Fprintln(out)
		for _, d := range c.Gaps() {
			fmt.Fprintf(out, "    %s: %s, %d files captured: %s\n", d.Datastore, strings.ToUpper(d.Status), d.Files, d.Message)
		}
	}
}

// fileInventoryWarnings names every datastore whose file list is incomplete.
// A datastore with no rows must never read as "no files".
func fileInventoryWarnings(coverage []assessment.FileInventoryContext) []string {
	var warnings []string
	for _, c := range coverage {
		if !c.Requested {
			continue
		}
		for _, d := range c.Gaps() {
			warnings = append(warnings, fmt.Sprintf("%s vFileInfo: datastore %s is %s (%d files captured): %s; absence of a file here is not evidence it does not exist", c.Context, d.Datastore, d.Status, d.Files, d.Message))
		}
	}
	return warnings
}
