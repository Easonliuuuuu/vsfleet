package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/report"
)

// filterProfile narrows the profile to one worksheet, matched without regard
// to case so "vinfo" finds vInfo.
func filterProfile(profile []report.SheetSpec, sheet string) ([]report.SheetSpec, error) {
	for _, spec := range profile {
		if strings.EqualFold(spec.Name, sheet) {
			return []report.SheetSpec{spec}, nil
		}
	}
	names := make([]string, 0, len(profile))
	for _, spec := range profile {
		names = append(names, spec.Name)
	}
	return nil, fmt.Errorf("unknown worksheet %q (this profile writes: %s)", sheet, strings.Join(names, ", "))
}

func printCompatibilityProfile(a *App, profile []report.SheetSpec) {
	out := a.out()
	for i, spec := range profile {
		if i > 0 {
			fmt.Fprintln(out)
		}
		origin := "vsfleet extension"
		if spec.Compatible {
			origin = "RVTools-named layout"
		}
		if spec.NotWritten {
			fmt.Fprintf(out, "%s  (RVTools worksheet, not written)\n", spec.Name)
		} else {
			fmt.Fprintf(out, "%s  (%d columns, %s, from %s)\n", spec.Name, len(spec.Columns), origin, spec.DerivesFrom)
		}
		if spec.Note != "" {
			for _, line := range wrapText(spec.Note, 76) {
				fmt.Fprintf(out, "  %s\n", line)
			}
		}
		fmt.Fprintln(out)
		// A column's explanation is a sentence, not a cell: in a table it
		// either wraps into illegibility or pushes every other column off an
		// 80-column terminal. Each column gets its own block instead, so the
		// output stays readable at any width and diffs cleanly between
		// releases.
		for _, col := range spec.Columns {
			printColumnSpec(out, col)
		}
		if len(spec.Columns) > 0 && len(spec.Omitted) > 0 {
			fmt.Fprintln(out)
		}
		printOmitted(out, spec.Omitted)
	}
}

// printOmitted writes the RVTools columns a worksheet leaves out, grouping
// consecutive columns that share a reason so the usage figures read as one
// list rather than thirty repeated paragraphs.
func printOmitted(out io.Writer, omitted []report.OmittedColumn) {
	if len(omitted) == 0 {
		return
	}
	fmt.Fprintln(out, "  Left out:")
	for i := 0; i < len(omitted); {
		j := i
		var names []string
		for ; j < len(omitted) && omitted[j].Reason == omitted[i].Reason; j++ {
			names = append(names, omitted[j].Name)
		}
		for _, line := range wrapNames(names, 74) {
			fmt.Fprintf(out, "  %s\n", line)
		}
		for _, line := range wrapText(omitted[i].Reason, 72) {
			fmt.Fprintf(out, "      %s\n", line)
		}
		i = j
	}
}

// printColumnSpec writes one column's block: its name, type and unit, then
// what it holds and when it is left empty.
func printColumnSpec(out io.Writer, col report.ColumnSpec) {
	kind := col.Kind
	if col.Unit != "" {
		kind += ", " + col.Unit
	}
	fmt.Fprintf(out, "  %s  [%s]\n", col.Name, kind)
	if col.Note != "" {
		for _, line := range wrapText(col.Note, 72) {
			fmt.Fprintf(out, "      %s\n", line)
		}
	}
	if col.Empty != "" {
		for _, line := range wrapText("Empty "+col.Empty+".", 72) {
			fmt.Fprintf(out, "      %s\n", line)
		}
	}
}

// wrapText breaks s into lines of at most width characters, so a worksheet's
// explanation stays readable in a terminal without the table below it being
// pushed sideways.
func wrapText(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	line := words[0]
	for _, word := range words[1:] {
		if len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = word
			continue
		}
		line += " " + word
	}
	return append(lines, line)
}

// wrapNames joins column names with ", " into lines of at most width
// characters, breaking only between names, since a column name such as
// "CPU reservationUsedForVm" holds a space of its own.
func wrapNames(names []string, width int) []string {
	var lines []string
	line := ""
	for i, name := range names {
		item := name
		if i < len(names)-1 {
			item += ","
		}
		if line != "" && len(line)+1+len(item) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += item
	}
	return append(lines, line)
}
