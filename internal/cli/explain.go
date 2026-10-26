package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/apiresource"
	"github.com/easonliuuuuu/vsfleet/internal/report"
)

// rvtoolsResource is the pseudo-kind "explain" answers for the whole export
// profile, so "explain rvtools.vInfo" reads like "explain vm.cpu".
const rvtoolsResource = "rvtools"

// kindSheets are the RVTools worksheets whose rows come from each kind.
// TestKindSheetsExist keeps the names in step with the export profile.
var kindSheets = map[string][]string{
	"vm":           {"vInfo", "vCPU", "vMemory", "vDisk", "vPartition", "vNetwork", "vCD", "vUSB", "vTools"},
	"template":     {"vInfo"},
	"host":         {"vHost"},
	"cluster":      {"vCluster"},
	"vapp":         {"vRP"},
	"resourcepool": {"vRP"},
	"datastore":    {"vDatastore"},
	"dvswitch":     {"dvSwitch"},
	"dvportgroup":  {"dvPort"},
	"snapshot":     {"vSnapshot"},
	"hba":          {"vHBA"},
	"pnic":         {"vNIC"},
	"vswitch":      {"vSwitch"},
	"portgroup":    {"vPort"},
	"vmkernel":     {"vSC_VMK"},
	"multipath":    {"vMultiPath"},
}

// kindExplanation is what "explain KIND -o json" writes.
type kindExplanation struct {
	Kind        apiresource.Kind `json:"kind"`
	GetColumns  []string         `json:"get_columns,omitempty"`
	WideColumns []string         `json:"wide_columns,omitempty"`
	// NarrowBy is the flag that narrows "get" to one parent, e.g. "host".
	NarrowBy string             `json:"narrow_by,omitempty"`
	Describe bool               `json:"describe"`
	RVTools  []report.SheetSpec `json:"rvtools,omitempty"`
}

// fieldMatch is one RVTools column a KIND.FIELD question found.
type fieldMatch struct {
	Sheet  string            `json:"sheet"`
	Column report.ColumnSpec `json:"column"`
}

// fieldExplanation is what "explain KIND.FIELD -o json" writes.
type fieldExplanation struct {
	Kind      string       `json:"kind"`
	Field     string       `json:"field"`
	GetColumn bool         `json:"get_column"`
	RVTools   []fieldMatch `json:"rvtools,omitempty"`
}

func newExplainCommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "explain KIND[.FIELD] | rvtools[.SHEET]",
		Short: "Document a kind's columns and the RVTools worksheets it feeds",
		Long: strings.TrimSpace(`
Document what vsfleet reports for a kind, the way "kubectl explain" does: the
columns "vsfleet get" shows, whether "vsfleet describe" accepts it, and every
RVTools worksheet the export writes from it, with each column's type, unit
and when it is left empty.

KIND.FIELD documents one column, matched without regard to case against the
kind's "get" columns and its worksheets' columns. Quote a field that holds a
space: 'vm.In Use MiB'.

"rvtools" documents the whole export profile, and "rvtools.SHEET" one
worksheet. That includes observed differences from RVTools in shared
columns; it does not describe the complete RVTools schema. Reads no
configuration, opens no keyring, and contacts no vCenter.`),
		Example: `  # What vsfleet reports for VMs
  vsfleet explain vm

  # One column, across every worksheet that has it
  vsfleet explain vm.annotation

  # Every worksheet and column the RVTools export writes
  vsfleet explain rvtools

  # One worksheet, as JSON, for a pipeline to validate against
  vsfleet explain rvtools.vInfo -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			resource, field, _ := strings.Cut(args[0], ".")
			profile, err := report.Profile()
			if err != nil {
				return err
			}
			if strings.EqualFold(resource, rvtoolsResource) {
				if field != "" {
					if profile, err = filterProfile(profile, field); err != nil {
						return err
					}
				}
				if a.json() {
					return writeJSON(a.out(), profile)
				}
				printCompatibilityProfile(a, profile)
				return nil
			}
			kind, err := lookupKind(resource)
			if err != nil {
				return err
			}
			e := explainKind(kind, profile)
			if field != "" {
				return explainField(a, e, field)
			}
			if a.json() {
				return writeJSON(a.out(), e)
			}
			printKindExplanation(a, e)
			return nil
		},
	}
}

func explainKind(kind apiresource.Kind, profile []report.SheetSpec) kindExplanation {
	e := kindExplanation{Kind: kind}
	if l, ok := listers[kind.Name]; ok {
		headers, wide := l.columns()
		e.GetColumns = headers
		if wide {
			e.WideColumns = []string{"TAGS", "CUSTOM ATTRIBUTES"}
		}
		e.NarrowBy = l.scope()
	}
	_, e.Describe = describers[kind.Name]
	for _, name := range kindSheets[kind.Name] {
		for _, spec := range profile {
			if spec.Name == name {
				e.RVTools = append(e.RVTools, spec)
			}
		}
	}
	return e
}

func explainField(a *App, e kindExplanation, field string) error {
	out := fieldExplanation{Kind: e.Kind.Name, Field: field}
	for _, h := range e.GetColumns {
		if strings.EqualFold(h, field) {
			out.GetColumn = true
		}
	}
	for _, spec := range e.RVTools {
		for _, col := range spec.Columns {
			if strings.EqualFold(col.Name, field) {
				out.RVTools = append(out.RVTools, fieldMatch{Sheet: spec.Name, Column: col})
			}
		}
	}
	if !out.GetColumn && len(out.RVTools) == 0 {
		return fmt.Errorf("%s has no field %q; run \"vsfleet explain %s\" for its fields", e.Kind.Name, field, e.Kind.Name)
	}
	if a.json() {
		return writeJSON(a.out(), out)
	}
	w := a.out()
	fmt.Fprintf(w, "KIND:   %s\nFIELD:  %s\n", e.Kind.Name, field)
	if out.GetColumn {
		fmt.Fprintf(w, "\nA column of \"vsfleet get %s\".\n", e.Kind.Plural)
	}
	for _, m := range out.RVTools {
		fmt.Fprintf(w, "\nRVTools %s:\n", m.Sheet)
		printColumnSpec(w, m.Column)
	}
	return nil
}

func printKindExplanation(a *App, e kindExplanation) {
	w := a.out()
	f := newFields(w)
	f.add("KIND:", e.Kind.Name)
	f.add("PLURAL:", e.Kind.Plural)
	f.add("SHORTNAMES:", strings.Join(e.Kind.ShortNames, ", "))
	f.add("SOURCE:", kindSource(e.Kind))
	f.add("DESCRIPTION:", e.Kind.Description)
	f.flush()
	if len(e.GetColumns) > 0 {
		fmt.Fprintf(w, "\nGET COLUMNS (vsfleet get %s):\n  %s\n", e.Kind.Plural, strings.Join(e.GetColumns, ", "))
		if len(e.WideColumns) > 0 {
			fmt.Fprintf(w, "  --wide adds %s\n", strings.Join(e.WideColumns, ", "))
		}
		if e.NarrowBy != "" {
			fmt.Fprintf(w, "  --%[1]s %[2]s narrows to one %[1]s\n", e.NarrowBy, strings.ToUpper(e.NarrowBy))
		}
	}
	if e.Describe {
		fmt.Fprintf(w, "\nDESCRIBE:\n  vsfleet describe %s NAME\n", e.Kind.Name)
	}
	if len(e.RVTools) > 0 {
		fmt.Fprintln(w, "\nRVTOOLS WORKSHEETS:")
		for _, spec := range e.RVTools {
			names := make([]string, len(spec.Columns))
			for i, col := range spec.Columns {
				names[i] = col.Name
			}
			fmt.Fprintf(w, "  %s (%d columns)\n", spec.Name, len(spec.Columns))
			for _, line := range wrapText(strings.Join(names, ", "), 72) {
				fmt.Fprintf(w, "      %s\n", line)
			}
		}
		fmt.Fprintf(w, "\nRun \"vsfleet explain %s.FIELD\" or \"vsfleet explain rvtools.SHEET\" for what a column holds.\n", e.Kind.Name)
	}
}
