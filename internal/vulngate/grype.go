package vulngate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

type grypeDocument struct {
	Matches    []grypeMatch `json:"matches"`
	Source     *grypeSource `json:"source"`
	Descriptor struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		DB      struct {
			Status *struct {
				Built string `json:"built"`
				Valid bool   `json:"valid"`
				Error string `json:"error"`
			} `json:"status"`
		} `json:"db"`
	} `json:"descriptor"`
}

type grypeSource struct {
	Type   string `json:"type"`
	Target struct {
		UserInput string `json:"userInput"`
	} `json:"target"`
}

type grypeMatch struct {
	Vulnerability struct {
		ID       string `json:"id"`
		Severity string `json:"severity"`
		Fix      struct {
			Versions []string `json:"versions"`
			State    string   `json:"state"`
		} `json:"fix"`
	} `json:"vulnerability"`
	RelatedVulnerabilities []struct {
		ID string `json:"id"`
	} `json:"relatedVulnerabilities"`
	Artifact struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Type    string `json:"type"`
	} `json:"artifact"`
}

// ParseGrype reads grype -o json output. A finding blocks when it is High
// or Critical and a fixed version exists; unfixed and lower-severity
// findings are reported for triage without blocking.
func ParseGrype(r io.Reader) (Scan, error) {
	var doc grypeDocument
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return Scan{}, fmt.Errorf("read grype output: %w", err)
	}
	if doc.Descriptor.Name != "grype" {
		return Scan{}, fmt.Errorf("output is from %q, not grype", doc.Descriptor.Name)
	}
	status := doc.Descriptor.DB.Status
	if status == nil {
		return Scan{}, errors.New("grype output has no database status; the scan did not load a database")
	}
	if !status.Valid {
		return Scan{}, fmt.Errorf("grype database is not valid: %s", status.Error)
	}
	built, err := time.Parse(time.RFC3339, status.Built)
	if err != nil {
		return Scan{}, fmt.Errorf("grype database date %q: %w", status.Built, err)
	}
	scan := Scan{Scanner: "grype", Version: doc.Descriptor.Version, DBBuilt: built}
	if doc.Source != nil {
		scan.Target = doc.Source.Type + " " + doc.Source.Target.UserInput
	}
	seen := map[string]bool{}
	for _, m := range doc.Matches {
		v := m.Vulnerability
		key := v.ID + "\x00" + m.Artifact.Name + "\x00" + m.Artifact.Version
		if seen[key] {
			continue
		}
		seen[key] = true
		finding := Finding{
			ID:       v.ID,
			Artifact: m.Artifact.Name,
			Version:  m.Artifact.Version,
			Fixed:    strings.Join(v.Fix.Versions, ", "),
			Severity: v.Severity,
		}
		for _, rel := range m.RelatedVulnerabilities {
			if rel.ID != v.ID {
				finding.Aliases = append(finding.Aliases, rel.ID)
			}
		}
		highOrCritical := v.Severity == "High" || v.Severity == "Critical"
		fixed := v.Fix.State == "fixed"
		switch {
		case highOrCritical && fixed:
			finding.Blocking = true
			finding.Reason = "fixable " + v.Severity
		case highOrCritical:
			finding.Reason = v.Severity + ", no fix (" + orUnknown(v.Fix.State) + ")"
		default:
			finding.Reason = orUnknown(v.Severity) + " severity is below the gate"
		}
		scan.Findings = append(scan.Findings, finding)
	}
	sortFindings(scan.Findings)
	return scan, nil
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
