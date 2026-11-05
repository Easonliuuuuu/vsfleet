package assessment

import (
	"regexp"
	"strings"
)

// importGaps drops, from a diff between an RVTools import and a live
// capture, the field changes that only say the workbook did not carry a
// field. The importer leaves a field it did not read empty, so a value on the
// live side against an empty one on the import side is a gap in the source,
// not a change to vCenter. Fields both sides recorded are still compared.
// Between two captures of the same kind nothing is dropped.
type importGaps struct {
	base, target bool
	// leftOut holds each dropped field, with device keys removed, so the diff
	// can say what it did not compare. inexact holds the fields dropped
	// because the workbook records them less precisely.
	leftOut, inexact map[string]bool
}

// importInexactFields are the VM and host fields RVTools records in the
// collector's local time and to the second, so an import never equals a live
// capture on them.
var importInexactFields = map[string]bool{"settings.create_date": true, "details.boot_time": true}

// importUncarriedFields are the fields an RVTools workbook never records in
// full: vInfo lists only the rules between VMs, and not whether a rule is
// enabled or mandatory, and vMultiPath lists only the LUNs a datastore is
// on, so a host's whole set of LUNs is unknown.
var importUncarriedFields = map[string]bool{clusterHostRulesField: true, clusterRuleStateField: true, "multipaths": true}

func newImportGaps(base, target Run) *importGaps {
	return &importGaps{base: base.Imported(), target: target.Imported(), leftOut: map[string]bool{}, inexact: map[string]bool{}}
}

func (g *importGaps) active() bool { return g.base != g.target }

// vmChanges filters the fields of each VM change and drops a change left
// with nothing to report.
func (g *importGaps) vmChanges(changes []VMChange) []VMChange {
	if !g.active() {
		return changes
	}
	out := changes[:0]
	for _, c := range changes {
		c.Fields = g.fields(c.Fields, vmFieldCarried)
		if c.Changes = g.modified(c.Changes, len(c.Fields)); len(c.Changes) > 0 {
			out = append(out, c)
		}
	}
	return out
}

// resourceChanges does the same for infrastructure changes.
func (g *importGaps) resourceChanges(changes []ResourceChange) []ResourceChange {
	if !g.active() {
		return changes
	}
	out := changes[:0]
	for _, c := range changes {
		c.Fields = g.fields(c.Fields, func(field string) bool { return field == "name" })
		if c.Changes = g.modified(c.Changes, len(c.Fields)); len(c.Changes) > 0 {
			out = append(out, c)
		}
	}
	return out
}

// modified removes "modified" from a change whose fields were all dropped.
func (g *importGaps) modified(changes []string, fields int) []string {
	if fields > 0 {
		return changes
	}
	out := make([]string, 0, len(changes))
	for _, c := range changes {
		if c != "modified" {
			out = append(out, c)
		}
	}
	return out
}

// fields keeps the changes that compare two recorded values. carried names
// the fields the importer always reads, whose empty value is a real one.
func (g *importGaps) fields(fields []FieldChange, carried func(string) bool) []FieldChange {
	var out []FieldChange
	for _, f := range fields {
		imported := f.After
		if g.base {
			imported = f.Before
		}
		// An import never records whether the VM's configuration was
		// readable; it records what the workbook held.
		unread := f.Field == migrationFieldPrefix+".configuration_available"
		if importInexactFields[f.Field] {
			g.inexact[f.Field] = true
			continue
		}
		if unread || importUncarriedFields[f.Field] || (!carried(f.Field) && emptyValue(imported)) {
			g.leftOut[deviceKey.ReplaceAllString(f.Field, "[]")] = true
			continue
		}
		out = append(out, f)
	}
	return out
}

// warning names the fields the diff did not compare.
func (g *importGaps) warning() string {
	var parts []string
	if len(g.leftOut) > 0 {
		parts = append(parts, "fields the RVTools workbook does not carry were not compared: "+strings.Join(sortedKeys(g.leftOut), ", "))
	}
	if len(g.inexact) > 0 {
		parts = append(parts, "fields the RVTools workbook records in local time to the second were not compared: "+strings.Join(sortedKeys(g.inexact), ", "))
	}
	return strings.Join(parts, "; ")
}

// vmFieldCarried reports the VM fields vInfo always carries; every other
// field is compared only when the import recorded it.
func vmFieldCarried(field string) bool {
	return !strings.HasPrefix(field, migrationFieldPrefix+".")
}

var deviceKey = regexp.MustCompile(`\[-?\d+\]`)

// emptyValue reports a field value that records nothing: an empty string,
// JSON null or an empty JSON string, list or object.
func emptyValue(v string) bool {
	switch v {
	case "", `""`, "null", "[]", "{}":
		return true
	}
	return false
}
