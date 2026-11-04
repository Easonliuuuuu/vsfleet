package vulngate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// govulncheck -format json writes a stream of objects, each holding one of
// these fields.
type govulncheckMessage struct {
	Config  *govulncheckConfig  `json:"config"`
	OSV     *govulncheckOSV     `json:"osv"`
	Finding *govulncheckFinding `json:"finding"`
}

type govulncheckConfig struct {
	ScannerName    string `json:"scanner_name"`
	ScannerVersion string `json:"scanner_version"`
	DBLastModified string `json:"db_last_modified"`
	GoVersion      string `json:"go_version"`
	ScanLevel      string `json:"scan_level"`
	ScanMode       string `json:"scan_mode"`
}

type govulncheckOSV struct {
	ID      string   `json:"id"`
	Aliases []string `json:"aliases"`
}

type govulncheckFinding struct {
	OSV          string             `json:"osv"`
	FixedVersion string             `json:"fixed_version"`
	Trace        []govulncheckFrame `json:"trace"`
}

type govulncheckFrame struct {
	Module   string `json:"module"`
	Version  string `json:"version"`
	Package  string `json:"package"`
	Function string `json:"function"`
	Receiver string `json:"receiver"`
}

func (f govulncheckFrame) symbol() string {
	name := f.Function
	if f.Receiver != "" {
		name = strings.TrimPrefix(f.Receiver, "*") + "." + name
	}
	return f.Package + "." + name
}

// reach is the strongest evidence seen for one advisory in one module
// version; govulncheck reports module, package, and symbol findings
// separately.
type reach struct {
	level int
	frame govulncheckFrame
	fixed string
	osv   string
}

// Reachability levels, from weakest to strongest evidence.
const (
	levelModule = iota + 1
	levelPackage
	// levelUnverified is a binary-mode finding for an advisory that covers
	// a whole package, in a binary without a symbol table (built with -s).
	// govulncheck cannot tell whether the package is linked in, so it
	// reports a wildcard such as "golang.org/x/crypto/openpgp/*" whether or
	// not the package is present.
	levelUnverified
	levelSymbol
)

// ParseGovulncheck reads govulncheck -format json output. A finding blocks
// when its trace reaches a vulnerable symbol: in source mode the program
// calls it, in binary mode the linker kept it. Findings that only import the
// package or require the module are reported without blocking, as are
// wildcard findings in stripped binaries, which the source scan settles.
func ParseGovulncheck(r io.Reader) (Scan, error) {
	dec := json.NewDecoder(r)
	var (
		cfg   *govulncheckConfig
		osvs  = map[string]govulncheckOSV{}
		order []string
		best  = map[string]reach{}
	)
	for {
		var msg govulncheckMessage
		if err := dec.Decode(&msg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return Scan{}, fmt.Errorf("read govulncheck output: %w", err)
		}
		switch {
		case msg.Config != nil:
			cfg = msg.Config
		case msg.OSV != nil:
			osvs[msg.OSV.ID] = *msg.OSV
		case msg.Finding != nil:
			f := msg.Finding
			if f.OSV == "" || len(f.Trace) == 0 {
				return Scan{}, fmt.Errorf("govulncheck finding without advisory or trace")
			}
			frame := f.Trace[0]
			level := levelModule
			switch {
			case strings.HasSuffix(frame.Function, "*"):
				level = levelUnverified
			case frame.Function != "":
				level = levelSymbol
			case frame.Package != "":
				level = levelPackage
			}
			key := f.OSV + "\x00" + frame.Module + "\x00" + frame.Version
			prev, seen := best[key]
			if !seen {
				order = append(order, key)
			}
			if !seen || level > prev.level {
				best[key] = reach{level, frame, f.FixedVersion, f.OSV}
			}
		}
	}
	if cfg == nil {
		return Scan{}, errors.New("govulncheck output has no config record; the scan did not run")
	}
	if cfg.ScanLevel != "symbol" {
		return Scan{}, fmt.Errorf("govulncheck scan level is %q, want symbol; reachability is unknown", cfg.ScanLevel)
	}
	built, err := time.Parse(time.RFC3339, cfg.DBLastModified)
	if err != nil {
		return Scan{}, fmt.Errorf("govulncheck database date %q: %w", cfg.DBLastModified, err)
	}
	scan := Scan{
		Scanner: "govulncheck",
		Version: cfg.ScannerVersion,
		Target:  strings.TrimSuffix(cfg.ScanMode+" scan, "+cfg.GoVersion, ", "),
		DBBuilt: built,
	}
	for _, key := range order {
		b := best[key]
		osv := osvs[b.osv]
		finding := Finding{
			ID:       b.osv,
			Aliases:  osv.Aliases,
			Artifact: b.frame.Module,
			Version:  b.frame.Version,
			Fixed:    b.fixed,
		}
		switch b.level {
		case levelSymbol:
			finding.Blocking = true
			finding.Reason = b.frame.symbol() + " is reachable"
		case levelUnverified:
			finding.Reason = b.frame.Package + " may be linked; a stripped binary cannot show it, so the source scan decides"
		case levelPackage:
			finding.Reason = "package imported, vulnerable code not called"
		default:
			finding.Reason = "module required, vulnerable package not imported"
		}
		scan.Findings = append(scan.Findings, finding)
	}
	sortFindings(scan.Findings)
	return scan, nil
}
