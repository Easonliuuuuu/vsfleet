// Command vsfleet-vulngate decides whether a govulncheck or grype JSON
// report passes the repository's vulnerability policy. It writes a Markdown
// summary to standard output and exits 1 when the gate fails, 2 when the
// report or exception file cannot be read. docs/security-scanning.md
// describes the policy.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vulngate"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, time.Now()))
}

func run(args []string, stdout, stderr io.Writer, now time.Time) int {
	fs := flag.NewFlagSet("vsfleet-vulngate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "", "report format: govulncheck or grype")
	exceptionsPath := fs.String("exceptions", ".github/vulnerability-exceptions.toml", "exception file")
	maxAge := fs.Duration("max-db-age", 0, "oldest acceptable vulnerability database (default 336h for govulncheck, 120h for grype)")
	title := fs.String("title", "", "summary heading (default: the report path)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: vsfleet-vulngate -format govulncheck|grype [flags] REPORT.json")
		return 2
	}
	report := fs.Arg(0)
	if *title == "" {
		*title = report
	}

	var parse func(io.Reader) (vulngate.Scan, error)
	defaultAge := time.Duration(0)
	switch *format {
	case "govulncheck":
		parse, defaultAge = vulngate.ParseGovulncheck, 14*24*time.Hour
	case "grype":
		parse, defaultAge = vulngate.ParseGrype, 5*24*time.Hour
	default:
		fmt.Fprintf(stderr, "vsfleet-vulngate: -format must be govulncheck or grype, not %q\n", *format)
		return 2
	}
	if *maxAge == 0 {
		*maxAge = defaultAge
	}

	scan, err := readWith(report, parse)
	if err != nil {
		fmt.Fprintf(stdout, "## %s: FAILED\n\nThe scan produced no usable report: %v\n", *title, err)
		fmt.Fprintf(stderr, "vsfleet-vulngate: %v\n", err)
		return 2
	}
	exceptions, err := readWith(*exceptionsPath, vulngate.LoadExceptions)
	if err != nil {
		fmt.Fprintf(stderr, "vsfleet-vulngate: %v\n", err)
		return 2
	}

	res := vulngate.Evaluate(scan, exceptions, *maxAge, now)
	if err := res.WriteMarkdown(stdout, *title); err != nil {
		fmt.Fprintf(stderr, "vsfleet-vulngate: write summary: %v\n", err)
		return 2
	}
	if err := res.Err(); errors.Is(err, vulngate.ErrBlocked) {
		fmt.Fprintf(stderr, "vsfleet-vulngate: %s: %v\n", *title, err)
		return 1
	}
	return 0
}

func readWith[T any](path string, parse func(io.Reader) (T, error)) (T, error) {
	f, err := os.Open(path)
	if err != nil {
		var zero T
		return zero, err
	}
	defer f.Close()
	return parse(f)
}
