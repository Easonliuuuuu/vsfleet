package assessment

import (
	"fmt"
	"time"
)

type PolicyOptions struct {
	FailOn          []string
	MaxSnapshotAge  time.Duration
	RequireComplete bool
}

// EvaluatePolicy turns a diff into a stable result suitable for CI. It never
// changes the diff's human-facing warnings or counts.
func EvaluatePolicy(d Diff, opts PolicyOptions) PolicyResult {
	result := PolicyResult{Passed: true}
	add := func(rule, message string) {
		result.Passed = false
		result.Violations = append(result.Violations, PolicyViolation{Rule: rule, Message: message})
	}
	for _, rule := range opts.FailOn {
		switch rule {
		case "appeared":
			if d.Counts.Appeared > 0 {
				add(rule, fmt.Sprintf("%d VM(s) appeared", d.Counts.Appeared))
			}
		case "vanished":
			if d.Counts.Vanished > 0 {
				add(rule, fmt.Sprintf("%d VM(s) vanished", d.Counts.Vanished))
			}
		case "moved":
			if d.Counts.Moved > 0 {
				add(rule, fmt.Sprintf("%d VM(s) moved", d.Counts.Moved))
			}
		case "modified":
			if d.Counts.Modified > 0 {
				add(rule, fmt.Sprintf("%d VM(s) modified", d.Counts.Modified))
			}
		case "snapshot-created":
			if n := snapshotCount(d, "created"); n > 0 {
				add(rule, fmt.Sprintf("%d snapshot(s) created", n))
			}
		case "snapshot-removed":
			if n := snapshotCount(d, "removed"); n > 0 {
				add(rule, fmt.Sprintf("%d snapshot(s) removed", n))
			}
		case "snapshot-changed":
			if n := snapshotCount(d, "changed"); n > 0 {
				add(rule, fmt.Sprintf("%d snapshot(s) changed", n))
			}
		}
	}
	if opts.MaxSnapshotAge > 0 {
		for _, age := range d.SnapshotAges {
			if age.Age >= opts.MaxSnapshotAge {
				add("max-snapshot-age", fmt.Sprintf("snapshot %q on %q is %s old", age.Name, age.VMName, age.Age.Round(time.Second)))
				break
			}
		}
	}
	if opts.RequireComplete && (d.Base.Status != RunComplete || d.Target.Status != RunComplete || len(d.Coverage) > 0) {
		add("require-complete", "baseline and target must both be complete with no coverage gaps")
	}
	return result
}

func snapshotCount(d Diff, kind string) int {
	n := 0
	for _, s := range d.Snapshots {
		if s.Kind == kind {
			n++
		}
	}
	return n
}
