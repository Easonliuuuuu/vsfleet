package cli

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/health"
	"github.com/easonliuuuuu/vsfleet/internal/report"
)

// newExportCommand writes a stored capture out in another format. Every
// export reads the capture --run names and writes to stdout unless --file is
// given; none contacts a vCenter.
func newExportCommand(a *App) *cobra.Command {
	cmd := requireSubcommand(&cobra.Command{
		Use:   "export",
		Short: "Write a stored capture as an RVTools workbook, a report or metadata",
		Long: strings.TrimSpace(`
Write a stored capture out: as an RVTools-compatible workbook, as the
"describe capture" report, or as its tags and custom attributes.

Every export reads the capture --run names (an ID, a label, latest, previous
or a date; latest by default) and writes to stdout unless --file is given.
-n narrows an export to some contexts. None contacts a vCenter.`),
		Example: `  # RVTools workbook of the most recent capture
  vsfleet export rvtools --file estate.xlsx

  # The capture report of a labelled capture, as JSON
  vsfleet export report --run nightly -o json --file nightly.json

  # Every tag and custom attribute, as CSV
  vsfleet export metadata --format csv > metadata.csv`,
	})
	cmd.AddCommand(newExportRVToolsCommand(a), newExportReportCommand(a), newExportMetadataCommand(a))
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

func newExportRVToolsCommand(a *App) *cobra.Command {
	var run, format, file string
	var force, includeMetadata bool
	var share shareFlags
	cmd := &cobra.Command{Use: "rvtools", Short: "Export a stored capture as an RVTools XLSX workbook or CSV files", Long: strings.TrimSpace(`
Export a stored capture as a 25-sheet RVTools-compatible workbook, or as one
CSV file per RVTools tab with --format csv.

Without --file the workbook is written to stdout, which must not be a
terminal; the receipt then goes to stderr. --format csv writes several
files, so it needs --file naming a directory.`), Example: `  # 25-sheet RVTools workbook from the most recent capture
  vsfleet export rvtools --file estate.xlsx

  # The same workbook on stdout, e.g. into another tool
  vsfleet export rvtools > estate.xlsx

  # One CSV per RVTools tab, into a directory
  vsfleet export rvtools --format csv --file ./estate-csv

  # Export a labelled capture, replacing an existing file
  vsfleet export rvtools --run nightly --file estate.xlsx --force

  # See what the workbook will contain before exporting
  vsfleet explain rvtools

  # Preview a scoped sizing summary, then write it with pseudonymized names
  vsfleet export rvtools --profile sizing-summary --pseudonymize --preview
  vsfleet export rvtools --profile sizing-summary --pseudonymize \
    --pseudonymize-key-file ~/.config/vsfleet/share.key --file sizing.xlsx`, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		format = strings.ToLower(strings.TrimSpace(format))
		if err := share.validate(format); err != nil {
			return err
		}
		switch format {
		case "rvtools":
			if file != "" && filepath.Ext(file) != ".xlsx" {
				return fmt.Errorf("--file must have a .xlsx extension")
			}
		case "csv":
			if file == "" {
				return fmt.Errorf("--format csv writes one file per tab, so it needs --file naming a directory")
			}
			if filepath.Ext(file) == ".xlsx" {
				return fmt.Errorf("--file must be a directory for --format csv, not a .xlsx path")
			}
		default:
			return fmt.Errorf("unsupported export format %q (supported: rvtools, csv)", format)
		}
		toStdout := file == "" && !share.preview
		if toStdout {
			if a.json() {
				return fmt.Errorf("-o json prints the export receipt, so it needs --file")
			}
			if isTerminalWriter(a.out()) {
				return fmt.Errorf("refusing to write an XLSX workbook to a terminal; pass --file or redirect stdout")
			}
		}
		var shareKey []byte
		if share.enabled() && !share.preview && share.pseudonymize {
			key, err := readShareKey(share.keyFile, a.in())
			if err != nil {
				return err
			}
			shareKey = key
		}
		data, err := loadStoredRun(a, cmd, run)
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
		// Without --file the receipt goes to stderr, so it never mixes with
		// the workbook on stdout.
		receiptOut := a.out()
		publish := func(write func(io.Writer) error) (exportReceipt, error) {
			return publishXLSX(data.Run.ID, file, force, write)
		}
		if toStdout {
			receiptOut = a.errOut()
			publish = func(write func(io.Writer) error) (exportReceipt, error) {
				return writeXLSXTo(a.out(), data.Run.ID, write)
			}
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
			receipt, err = publish(func(w io.Writer) error {
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
			fmt.Fprintf(receiptOut, "capture %d exported to %s with profile %s (%d bytes, sha256 %s)\n", receipt.RunID, receipt.Path, plan.Profile, receipt.Bytes, receipt.SHA256)
			return nil
		}
		exportOpts := report.ExportOptions{Metadata: includeMetadata}
		if format == "csv" {
			receipt, err = publishRVToolsCSV(data, healthReport, exportOpts, file, force)
		} else {
			receipt, err = publish(func(w io.Writer) error {
				if err := report.WriteRVToolsWith(w, data, healthReport, exportOpts); err != nil {
					return fmt.Errorf("write RVTools export: %w", err)
				}
				return nil
			})
		}
		if err != nil {
			return err
		}
		receipt.FileInventory = inventory
		if a.json() {
			return writeJSON(a.out(), receipt)
		}
		if format == "csv" {
			fmt.Fprintf(receiptOut, "capture %d exported to %s (%d files)\n", receipt.RunID, receipt.Path, len(receipt.Files))
			for _, f := range receipt.Files {
				fmt.Fprintf(receiptOut, "  %s (%d bytes, sha256 %s)\n", f.Name, f.Bytes, f.SHA256)
			}
		} else {
			fmt.Fprintf(receiptOut, "capture %d exported to %s (%d bytes, sha256 %s)\n", receipt.RunID, receipt.Path, receipt.Bytes, receipt.SHA256)
		}
		return nil
	}}
	addExportRunFlag(cmd, &run)
	cmd.Flags().StringVar(&format, "format", "rvtools", "export format: rvtools (XLSX) or csv (one file per RVTools tab)")
	cmd.Flags().StringVar(&file, "file", "", "destination: a .xlsx file for --format rvtools (default stdout), a directory for --format csv")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing export")
	cmd.Flags().BoolVar(&includeMetadata, "include-metadata", false, "add the vsfleetMetadata sheet: one row per tag or custom attribute value (omitted by --profile sizing-summary)")
	share.add(cmd)
	return cmd
}

// addExportRunFlag gives an export its --run: the capture to write out.
func addExportRunFlag(cmd *cobra.Command, run *string) {
	cmd.Flags().StringVar(run, "run", "latest", "capture to export: ID, label, latest, previous or date")
}

// writeXLSXTo renders a workbook in memory and only then copies it to w, so
// a failed export never leaves half a workbook in a pipe.
func writeXLSXTo(w io.Writer, runID int64, write func(io.Writer) error) (exportReceipt, error) {
	var buf bytes.Buffer
	if err := write(&buf); err != nil {
		return exportReceipt{}, err
	}
	sum := sha256.Sum256(buf.Bytes())
	n, err := w.Write(buf.Bytes())
	if err != nil {
		return exportReceipt{}, fmt.Errorf("write export to stdout: %w", err)
	}
	return exportReceipt{RunID: runID, Path: "stdout", Bytes: int64(n), SHA256: fmt.Sprintf("%x", sum)}, nil
}

func newExportReportCommand(a *App) *cobra.Command {
	var run, file string
	var force bool
	cmd := &cobra.Command{Use: "report", Short: "Write the \"describe capture\" report of a stored capture", Long: strings.TrimSpace(`
Write the report "vsfleet describe capture" shows: the capture's status,
object counts, coverage per context and collection, performance
collections and warnings. -o json writes the same report as JSON.`), Example: `  # The most recent capture's report, to a file
  vsfleet export report --file capture.txt

  # A labelled capture's report as JSON, for an archive or a ticket
  vsfleet export report --run nightly -o json --file nightly.json`, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		objects, _, err := captureDescriber{}.resolve(cmd.Context(), a, []string{run})
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		if a.json() {
			if err := writeJSON(&buf, objects[0]); err != nil {
				return err
			}
		} else {
			captureDescriber{}.print(&buf, objects[0])
		}
		if err := writeOutput(a.out(), file, buf.Bytes(), force); err != nil {
			return err
		}
		if file != "" {
			fmt.Fprintf(a.errOut(), "capture %d report written to %s\n", objects[0].(captureReport).Run.ID, file)
		}
		return nil
	}}
	addExportRunFlag(cmd, &run)
	cmd.Flags().StringVar(&file, "file", "", "write the report to this file instead of stdout")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing --file")
	return cmd
}

// publishXLSX publishes the workbook write produces at file: written to a
// sibling temp file first, then published atomically (a rename with --force,
// otherwise a no-clobber hard link) so a failed export never leaves a
// half-written workbook.
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
		warnings = append(warnings, fmt.Sprintf("capture %d is %s; export coverage may be incomplete", data.Run.ID, data.Run.Status))
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
