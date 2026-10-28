package cli

import (
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

type policyExitError struct{ violations []assessment.PolicyViolation }

func (e *policyExitError) Error() string {
	return fmt.Sprintf("assessment diff policy failed (%d violation(s))", len(e.violations))
}
func (e *policyExitError) ExitCode() int { return 2 }

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

// newAssessmentRenamedCommand answers the removed "assessment" group, and
// each of its former subcommands, with where that job went.
func newAssessmentRenamedCommand() *cobra.Command {
	cmd := renamedCommand("assessment", []string{"assess", "history"},
		`use "vsfleet capture", "vsfleet analyze", "vsfleet export" or "vsfleet db"`)
	for _, r := range []struct{ name, hint string }{
		{"run", `use "vsfleet capture run"`},
		{"list", `use "vsfleet capture list" or "vsfleet get captures"`},
		{"diff", `use "vsfleet capture diff"`},
		{"delete", `use "vsfleet capture delete RUN"`},
		{"update", `use "vsfleet capture update RUN"`},
		{"prune", `use "vsfleet capture prune"`},
		{"report", `use "vsfleet describe capture RUN", or "vsfleet export report --run RUN" to write it to a file`},
		{"findings", `use "vsfleet analyze health"`},
		{"readiness", `use "vsfleet analyze readiness"`},
		{"network-readiness", `use "vsfleet analyze readiness network cl/SOURCE cl/TARGET"`},
		{"sizing", `use "vsfleet analyze sizing"`},
		{"capacity", `use "vsfleet analyze capacity"`},
		{"orphans", `use "vsfleet analyze orphans"`},
		{"snapshots", `use "vsfleet analyze snapshots"`},
		{"export", `use "vsfleet export rvtools --run RUN"`},
		{"metadata", `use "vsfleet export metadata --run RUN"`},
		{"metadata-report", `use "vsfleet export metadata --definition DEFINITION --run RUN"`},
		{"backup", `use "vsfleet db backup FILE"`},
		{"restore", `use "vsfleet db restore FILE"`},
		{"doctor", `use "vsfleet db doctor"`},
		{"inventory", `use "vsfleet get KIND --run RUN", e.g. "vsfleet get vms,hosts --run latest -A"`},
	} {
		cmd.AddCommand(renamedCommand(r.name, nil, r.hint))
	}
	cmd.AddCommand(renamedTrendsCommand(), renamedPerfCommand())
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
