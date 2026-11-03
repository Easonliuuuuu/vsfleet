package vulngate

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// ErrBlocked is returned by Result.Err when the gate fails.
var ErrBlocked = errors.New("vulnerability gate failed")

// Result is the gate's verdict on one scan.
type Result struct {
	Scan     Scan
	Now      time.Time
	MaxAge   time.Duration
	Blocking []Finding
	Excepted []Excepted
	Advisory []Finding
	// Failures are reasons the gate fails other than a finding: a stale
	// database or an unusable exception.
	Failures []string
}

// Excepted pairs a finding with the exception that accepted it.
type Excepted struct {
	Finding   Finding
	Exception Exception
}

// Evaluate applies the exceptions to the scan. The gate fails on any
// blocking finding no valid exception covers, on any invalid or expired
// exception, and on a database older than maxAge.
func Evaluate(scan Scan, exceptions []Exception, maxAge time.Duration, now time.Time) Result {
	res := Result{Scan: scan, Now: now, MaxAge: maxAge}
	if age := now.Sub(scan.DBBuilt); age > maxAge {
		res.Failures = append(res.Failures, fmt.Sprintf(
			"%s database was built %s, %s ago; the limit is %s",
			scan.Scanner, scan.DBBuilt.UTC().Format(time.RFC3339), age.Round(time.Hour), maxAge))
	}
	var valid []Exception
	for _, e := range exceptions {
		if problems := e.problems(now); len(problems) > 0 {
			res.Failures = append(res.Failures, fmt.Sprintf("exception %s: %s", e.label(), strings.Join(problems, "; ")))
			continue
		}
		valid = append(valid, e)
	}
	for _, f := range scan.Findings {
		if e, ok := firstCovering(valid, f); ok {
			res.Excepted = append(res.Excepted, Excepted{Finding: f, Exception: e})
			continue
		}
		if f.Blocking {
			res.Blocking = append(res.Blocking, f)
		} else {
			res.Advisory = append(res.Advisory, f)
		}
	}
	return res
}

func firstCovering(exceptions []Exception, f Finding) (Exception, bool) {
	for _, e := range exceptions {
		if e.covers(f) {
			return e, true
		}
	}
	return Exception{}, false
}

// Err returns ErrBlocked when the gate fails.
func (r Result) Err() error {
	if len(r.Blocking) > 0 || len(r.Failures) > 0 {
		return fmt.Errorf("%w: %d blocking finding(s), %d other failure(s)", ErrBlocked, len(r.Blocking), len(r.Failures))
	}
	return nil
}

// WriteMarkdown writes the verdict as a GitHub step summary section.
func (r Result) WriteMarkdown(w io.Writer, title string) error {
	var b strings.Builder
	verdict := "passed"
	if r.Err() != nil {
		verdict = "FAILED"
	}
	fmt.Fprintf(&b, "## %s: %s\n\n", title, verdict)
	fmt.Fprintf(&b, "%s %s, %s. Database built %s (limit %s).\n",
		r.Scan.Scanner, orUnknown(r.Scan.Version), orUnknown(r.Scan.Target),
		r.Scan.DBBuilt.UTC().Format(time.RFC3339), r.MaxAge)
	if len(r.Failures) > 0 {
		b.WriteString("\n### Gate failures\n\n")
		for _, f := range r.Failures {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}
	writeTable(&b, "Blocking findings", r.Blocking)
	if len(r.Excepted) > 0 {
		b.WriteString("\n### Accepted by exception\n\n| Advisory | Artifact | Version | Owner | Expires |\n| --- | --- | --- | --- | --- |\n")
		for _, x := range r.Excepted {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", x.Finding.ID, x.Finding.Artifact, x.Finding.Version, x.Exception.Owner, x.Exception.Expires)
		}
	}
	writeTable(&b, "Reported, not blocking", r.Advisory)
	if len(r.Blocking)+len(r.Excepted)+len(r.Advisory) == 0 {
		b.WriteString("\nNo findings.\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeTable(b *strings.Builder, heading string, findings []Finding) {
	if len(findings) == 0 {
		return
	}
	fmt.Fprintf(b, "\n### %s\n\n| Advisory | Artifact | Version | Fixed in | Severity | Why |\n| --- | --- | --- | --- | --- | --- |\n", heading)
	for _, f := range findings {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s |\n",
			cell(f.ID), cell(f.Artifact), cell(f.Version), cell(orNone(f.Fixed)), cell(orDash(f.Severity)), cell(f.Reason))
	}
}

func cell(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
