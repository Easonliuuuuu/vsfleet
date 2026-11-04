// Package vulngate turns govulncheck and grype JSON into one pass/fail
// verdict for CI. Each scanner keeps its own blocking rule: govulncheck
// blocks on vulnerable code the program calls, grype blocks on fixable High
// and Critical findings in the image. Reviewed exceptions silence a finding
// until they expire, and a scan that cannot prove its database is fresh
// fails instead of passing as clean.
package vulngate

import (
	"slices"
	"time"
)

// Scan is one scanner run reduced to what the gate decides on.
type Scan struct {
	Scanner  string
	Version  string
	Target   string
	DBBuilt  time.Time
	Findings []Finding
}

// Finding is one advisory affecting one artifact version.
type Finding struct {
	// ID is the scanner's primary advisory ID; Aliases holds the CVE,
	// GHSA, and GO IDs it is also known by.
	ID      string
	Aliases []string
	// Artifact is the Go module, "stdlib", or OS package name, and Version
	// the version the scan found.
	Artifact string
	Version  string
	Fixed    string
	Severity string
	// Blocking reports whether the scanner's rule fails the gate for this
	// finding; Reason says why, or why not.
	Blocking bool
	Reason   string
}

// IDs returns the primary ID followed by its aliases.
func (f Finding) IDs() []string {
	return append([]string{f.ID}, f.Aliases...)
}

func sortFindings(findings []Finding) {
	slices.SortFunc(findings, func(a, b Finding) int {
		if a.Blocking != b.Blocking {
			if a.Blocking {
				return -1
			}
			return 1
		}
		for _, c := range [][2]string{{a.Artifact, b.Artifact}, {a.ID, b.ID}, {a.Version, b.Version}} {
			if c[0] != c[1] {
				if c[0] < c[1] {
					return -1
				}
				return 1
			}
		}
		return 0
	})
}
