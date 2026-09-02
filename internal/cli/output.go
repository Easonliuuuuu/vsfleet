package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// Output formats.
const (
	FormatTable = "table"
	FormatJSON  = "json"
)

// Status glyphs used across the commands.
const (
	glyphOK      = "✓" // check mark
	glyphFail    = "✕" // ballot X
	glyphSkip    = "–" // en dash
	glyphOnline  = "●" // filled circle
	glyphPending = "◐" // half circle
	glyphOffline = "○" // empty circle
)

// table writes aligned columns. Padding is deliberately generous so that
// output stays readable when a vCenter name and a VM name sit side by side.
type table struct {
	w    *tabwriter.Writer
	cols int
}

func newTable(out io.Writer, headers ...string) *table {
	t := &table{w: tabwriter.NewWriter(out, 0, 4, 3, ' ', 0), cols: len(headers)}
	if len(headers) > 0 {
		fmt.Fprintln(t.w, strings.Join(headers, "\t"))
	}
	return t
}

func (t *table) row(cells ...string) {
	fmt.Fprintln(t.w, strings.Join(cells, "\t"))
}

func (t *table) flush() { _ = t.w.Flush() }

// writeJSON renders v as indented JSON, which is what makes vctui scriptable.
func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// fields writes a two-column "label value" block, the shape used by the
// detail and diagnostic commands.
type fields struct {
	w *tabwriter.Writer
}

func newFields(out io.Writer) *fields {
	return &fields{w: tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)}
}

func (f *fields) add(label, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(f.w, "  %s\t%s\n", label, value)
}

func (f *fields) flush() { _ = f.w.Flush() }

// humanMB renders a memory size the way an operator says it out loud: 32G,
// not 32768.
func humanMB(mb int64) string {
	switch {
	case mb <= 0:
		return "-"
	case mb >= 1<<20 && mb%(1<<20) == 0:
		return strconv.FormatInt(mb/(1<<20), 10) + "T"
	case mb >= 1024:
		g := float64(mb) / 1024
		if g == float64(int64(g)) {
			return strconv.FormatInt(int64(g), 10) + "G"
		}
		return strconv.FormatFloat(g, 'f', 1, 64) + "G"
	default:
		return strconv.FormatInt(mb, 10) + "M"
	}
}

// humanBytes renders a storage size with binary units.
func humanBytes(b int64) string {
	if b <= 0 {
		return "-"
	}
	const unit = 1024
	units := []string{"K", "M", "G", "T", "P"}
	if b < unit {
		return strconv.FormatInt(b, 10) + "B"
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit && exp < len(units)-1; n /= unit {
		div *= unit
		exp++
	}
	v := float64(b) / float64(div)
	if v >= 100 {
		return strconv.FormatFloat(v, 'f', 0, 64) + units[exp]
	}
	return strconv.FormatFloat(v, 'f', 1, 64) + units[exp]
}

// humanDuration renders a latency in the unit that keeps it readable.
func humanDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return "-"
	case d < time.Millisecond:
		return strconv.FormatInt(d.Microseconds(), 10) + " us"
	case d < time.Second:
		return strconv.FormatInt(d.Milliseconds(), 10) + " ms"
	default:
		return strconv.FormatFloat(d.Seconds(), 'f', 2, 64) + " s"
	}
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func itoa(n int) string { return strconv.Itoa(n) }

func i32toa(n int32) string { return strconv.FormatInt(int64(n), 10) }
