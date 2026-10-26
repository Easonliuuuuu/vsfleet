package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/easonliuuuuu/vsfleet/internal/jsonpath"
)

// The output formats only "get" takes, beyond table and json.
const (
	FormatWide          = "wide"
	FormatYAML          = "yaml"
	FormatName          = "name"
	FormatCSV           = "csv"
	FormatJSONPath      = "jsonpath"
	FormatCustomColumns = "custom-columns"
)

// annotationAllOutputs marks a command that takes every -o format; the rest
// take table and json only.
const annotationAllOutputs = "vsfleet.io/all-output-formats"

// outputSpec is a parsed -o value: a format and, for jsonpath= and
// custom-columns=, its argument.
type outputSpec struct {
	format string
	arg    string
}

func parseOutput(s string) (outputSpec, error) {
	name, arg, hasArg := strings.Cut(s, "=")
	switch name {
	case "", FormatTable, FormatJSON, FormatWide, FormatYAML, FormatName, FormatCSV:
		if hasArg {
			return outputSpec{}, fmt.Errorf("-o %s takes no argument", name)
		}
		if name == "" {
			name = FormatTable
		}
		return outputSpec{format: name}, nil
	case FormatJSONPath, FormatCustomColumns:
		if arg == "" {
			return outputSpec{}, fmt.Errorf("-o %[1]s needs a template, as in -o %[1]s=TEMPLATE", name)
		}
		return outputSpec{format: name, arg: arg}, nil
	}
	return outputSpec{}, fmt.Errorf("unknown output format %q; every command takes table and json, and \"get\" also takes wide, yaml, name, csv, jsonpath=TEMPLATE and custom-columns=SPEC", s)
}

// checkOutput rejects a format the command does not render, so "-o yaml"
// never quietly prints a table.
func (a *App) checkOutput(cmd *cobra.Command) error {
	spec, err := parseOutput(a.Format)
	if err != nil {
		return err
	}
	if spec.format == FormatTable || spec.format == FormatJSON || cmd.Annotations[annotationAllOutputs] != "" {
		return nil
	}
	return fmt.Errorf("-o %s is not supported by %q, which takes table or json", spec.format, cmd.CommandPath())
}

// render writes the listings in the requested format. single is true when
// exactly one object was asked for by name, which is when kubectl hands
// jsonpath the object itself rather than a List.
func render(w io.Writer, spec outputSpec, listings []*kindListing, noHeaders, single bool) error {
	switch spec.format {
	case FormatJSON:
		return writeJSON(w, listingsValue(listings))
	case FormatYAML:
		return writeYAML(w, listingsValue(listings))
	case FormatName:
		for _, l := range listings {
			for _, name := range l.names {
				fmt.Fprintf(w, "%s/%s\n", l.kind.Name, name)
			}
		}
		return nil
	case FormatCSV:
		return writeCSV(w, listings, noHeaders)
	case FormatJSONPath:
		return writeJSONPath(w, spec.arg, listings, single)
	case FormatCustomColumns:
		return writeCustomColumns(w, spec.arg, listings, noHeaders)
	}
	printListings(w, listings, noHeaders)
	return nil
}

// listingsValue is the JSON or YAML document: an array for one kind, an
// object of arrays keyed by plural for several.
func listingsValue(listings []*kindListing) any {
	if len(listings) == 1 {
		return listings[0].raw
	}
	byKind := make(map[string][]json.RawMessage, len(listings))
	for _, l := range listings {
		byKind[l.kind.Plural] = l.raw
	}
	return byKind
}

// printListings writes one table per kind, a blank line apart, the way
// "kubectl get pods,services" does.
func printListings(w io.Writer, listings []*kindListing, noHeaders bool) {
	printed := 0
	for _, l := range listings {
		if len(l.rows) == 0 {
			continue
		}
		if printed > 0 {
			fmt.Fprintln(w)
		}
		var t *table
		if noHeaders {
			t = newTable(w)
		} else {
			t = newTable(w, l.headers...)
		}
		for _, row := range l.rows {
			t.row(row...)
		}
		t.flush()
		printed++
	}
}

// writeYAML renders v as YAML in the order JSON gives its fields, which is
// the order the domain types declare them; a map would sort them.
func writeYAML(w io.Writer, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	blockStyle(&doc)
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	return enc.Close()
}

// blockStyle clears the flow and quoting styles JSON input leaves on every
// node, so the encoder writes ordinary block YAML and quotes only where YAML
// needs it.
func blockStyle(n *yaml.Node) {
	n.Style = 0
	for _, c := range n.Content {
		blockStyle(c)
	}
}

func writeCSV(w io.Writer, listings []*kindListing, noHeaders bool) error {
	if len(listings) != 1 {
		return errors.New("-o csv takes one kind, so every row has the same columns")
	}
	l := listings[0]
	cw := csv.NewWriter(w)
	if !noHeaders {
		if err := cw.Write(l.headers); err != nil {
			return err
		}
	}
	if err := cw.WriteAll(l.rows); err != nil {
		return err
	}
	return cw.Error()
}

func writeJSONPath(w io.Writer, template string, listings []*kindListing, single bool) error {
	tmpl, err := jsonpath.Parse(template)
	if err != nil {
		return err
	}
	var items []any
	for _, l := range listings {
		items = append(items, l.objects...)
	}
	var data any = map[string]any{"kind": "List", "items": items}
	if single && len(items) == 1 {
		data = items[0]
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, data); err != nil {
		return err
	}
	// kubectl ends the output where the template does; a terminal prompt
	// after it is the template's business.
	_, err = w.Write(b.Bytes())
	return err
}

// customColumn is one NAME:EXPR pair of -o custom-columns.
type customColumn struct {
	header string
	expr   *jsonpath.Template
}

func parseCustomColumns(spec string) ([]customColumn, error) {
	var cols []customColumn
	for _, part := range splitOutsideBraces(spec) {
		header, expr, ok := strings.Cut(part, ":")
		if !ok || header == "" || expr == "" {
			return nil, fmt.Errorf("custom column %q is not NAME:EXPRESSION, e.g. NAME:.name", part)
		}
		tmpl, err := jsonpath.ParseExpr(expr)
		if err != nil {
			return nil, err
		}
		cols = append(cols, customColumn{header: header, expr: tmpl})
	}
	return cols, nil
}

// splitOutsideBraces splits a custom-columns spec at commas that are not
// inside a {...} or [...], so a filter such as [?(@.a in (x,y))] survives.
func splitOutsideBraces(s string) []string {
	var (
		out   []string
		start int
		depth int
	)
	for i, c := range s {
		switch c {
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

func writeCustomColumns(w io.Writer, spec string, listings []*kindListing, noHeaders bool) error {
	cols, err := parseCustomColumns(spec)
	if err != nil {
		return err
	}
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = c.header
	}
	var t *table
	if noHeaders {
		t = newTable(w)
	} else {
		t = newTable(w, headers...)
	}
	for _, l := range listings {
		for _, obj := range l.objects {
			row := make([]string, len(cols))
			for i, c := range cols {
				row[i] = columnValue(c.expr.Values(obj))
			}
			t.row(row...)
		}
	}
	t.flush()
	return nil
}

// columnValue joins what an expression selected with commas, and shows
// <none> for nothing, as kubectl does.
func columnValue(values []any) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		if s, err := jsonpath.Format(v); err == nil && s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return "<none>"
	}
	return strings.Join(parts, ",")
}

// sortRows orders a listing by what expr selects from each object: numbers
// numerically, strings lexically, objects without the field last. The sort
// is stable, so equal keys keep vCenter's order.
func sortRows(l *kindListing, expr *jsonpath.Template) {
	keys := make([]any, len(l.objects))
	has := make([]bool, len(l.objects))
	for i, obj := range l.objects {
		if v := expr.Values(obj); len(v) > 0 && v[0] != nil {
			keys[i], has[i] = v[0], true
		}
	}
	order := make([]int, len(l.objects))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(i, j int) int {
		if has[i] != has[j] {
			if has[i] {
				return -1
			}
			return 1
		}
		if !has[i] {
			return 0
		}
		if c, ok := jsonpath.Compare(keys[i], keys[j]); ok {
			return c
		}
		a, _ := jsonpath.Format(keys[i])
		b, _ := jsonpath.Format(keys[j])
		return strings.Compare(a, b)
	})
	l.rows = permute(l.rows, order)
	l.raw = permute(l.raw, order)
	l.objects = permute(l.objects, order)
	l.names = permute(l.names, order)
}

func permute[T any](in []T, order []int) []T {
	out := make([]T, len(order))
	for i, j := range order {
		out[i] = in[j]
	}
	return out
}
