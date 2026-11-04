package vulngate

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

var update = flag.Bool("update", false, "rewrite golden files")

// now is a fixed instant after the fixtures' database dates.
var now = time.Date(2030, 1, 15, 12, 0, 0, 0, time.UTC)

const day = 24 * time.Hour

func open(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func parseFixture(t *testing.T, parse func(r *os.File) (Scan, error), name string) Scan {
	t.Helper()
	scan, err := parse(open(t, name))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return scan
}

func govulncheck(r *os.File) (Scan, error) { return ParseGovulncheck(r) }
func grype(r *os.File) (Scan, error)       { return ParseGrype(r) }

func loadFixtureExceptions(t *testing.T) []Exception {
	t.Helper()
	exceptions, err := LoadExceptions(open(t, "exceptions.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return exceptions
}

// brief renders findings as "ID artifact@version block|info" lines.
func brief(findings []Finding) string {
	var lines []string
	for _, f := range findings {
		verdict := "info"
		if f.Blocking {
			verdict = "block"
		}
		lines = append(lines, f.ID+" "+f.Artifact+"@"+f.Version+" "+verdict+": "+f.Reason)
	}
	return strings.Join(lines, "\n")
}

func TestParseFindings(t *testing.T) {
	tests := []struct {
		name    string
		parse   func(*os.File) (Scan, error)
		fixture string
		want    string
	}{
		{
			name:    "govulncheck keeps the strongest reachability per advisory",
			parse:   govulncheck,
			fixture: "govulncheck-source.json",
			want: strings.Join([]string{
				"GO-2030-0001 example.test/parser@v1.2.3 block: example.test/parser/xml.Reader.Decode is reachable",
				"GO-2030-0004 stdlib@v1.40.1 block: net/http.Client.Do is reachable",
				"GO-2030-0003 example.test/crypto@v0.5.0 info: module required, vulnerable package not imported",
				"GO-2030-0002 example.test/net@v0.8.0 info: package imported, vulnerable code not called",
			}, "\n"),
		},
		{
			name:    "govulncheck binary scan downgrades stripped-binary wildcards",
			parse:   govulncheck,
			fixture: "govulncheck-binary.json",
			want:    "GO-2030-0005 example.test/crypto@v0.5.0 info: example.test/crypto/legacy may be linked; a stripped binary cannot show it, so the source scan decides",
		},
		{
			name:    "grype blocks only fixable High and Critical",
			parse:   grype,
			fixture: "grype-image.json",
			want: strings.Join([]string{
				"GHSA-aaaa-bbbb-cccc example.test/parser@v1.2.3 block: fixable High",
				"CVE-2030-2001 libc6@2.36-9+deb13u8 block: fixable Critical",
				"CVE-2030-2003 stdlib@go1.40.1 info: Medium severity is below the gate",
				"CVE-2030-2002 tzdata@2030a-1 info: High, no fix (not-fixed)",
			}, "\n"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scan := parseFixture(t, tt.parse, tt.fixture)
			if got := brief(scan.Findings); got != tt.want {
				t.Errorf("findings:\n%s\nwant:\n%s", got, tt.want)
			}
			if scan.DBBuilt.IsZero() {
				t.Error("database date not parsed")
			}
		})
	}
}

func TestParseKeepsAliases(t *testing.T) {
	scan := parseFixture(t, govulncheck, "govulncheck-source.json")
	got := strings.Join(scan.Findings[0].IDs(), ",")
	if want := "GO-2030-0001,CVE-2030-1001,GHSA-aaaa-bbbb-cccc"; got != want {
		t.Errorf("govulncheck IDs = %s, want %s", got, want)
	}
	scan = parseFixture(t, grype, "grype-image.json")
	got = strings.Join(scan.Findings[0].IDs(), ",")
	if want := "GHSA-aaaa-bbbb-cccc,CVE-2030-1001"; got != want {
		t.Errorf("grype IDs = %s, want %s", got, want)
	}
}

// A scanner that could not reach its database or never ran must not read
// as a clean scan.
func TestParseRejectsUnprovenScans(t *testing.T) {
	tests := []struct {
		name  string
		parse func(string) error
		input string
		want  string
	}{
		{"govulncheck empty output", parseGovulncheckString, "", "no config record"},
		{"govulncheck progress only", parseGovulncheckString, `{"progress":{"message":"Fetching vulnerabilities from the database..."}}`, "no config record"},
		{"govulncheck truncated", parseGovulncheckString, `{"config":{"scan_level":"symbol"`, "read govulncheck output"},
		{"govulncheck module level", parseGovulncheckString, `{"config":{"scan_level":"module","db_last_modified":"2030-01-10T00:00:00Z"}}`, "want symbol"},
		{"govulncheck no database date", parseGovulncheckString, `{"config":{"scan_level":"symbol"}}`, "database date"},
		{"grype empty output", parseGrypeString, "", "read grype output"},
		{"grype other tool", parseGrypeString, `{"descriptor":{"name":"syft"}}`, `from "syft"`},
		{"grype no database", parseGrypeString, `{"matches":[],"descriptor":{"name":"grype"}}`, "no database status"},
		{"grype invalid database", parseGrypeString, mustRead(t, "grype-invalid-db.json"), "unable to fetch database listing"},
		{"grype no database date", parseGrypeString, `{"descriptor":{"name":"grype","db":{"status":{"valid":true}}}}`, "database date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.parse(tt.input)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func parseGovulncheckString(s string) error {
	_, err := ParseGovulncheck(strings.NewReader(s))
	return err
}

func parseGrypeString(s string) error {
	_, err := ParseGrype(strings.NewReader(s))
	return err
}

func mustRead(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLoadExceptionsRejectsUnknownKeys(t *testing.T) {
	_, err := LoadExceptions(strings.NewReader("[[exception]]\nid = \"GO-2030-0001\"\nexpiry = 2030-02-01\n"))
	if err == nil || !strings.Contains(err.Error(), "expiry") {
		t.Errorf("error = %v, want one naming the unknown key", err)
	}
}

func TestEvaluate(t *testing.T) {
	valid := Exception{
		ID:            "GO-2030-0001",
		Artifact:      "example.test/parser",
		Versions:      []string{"v1.2.3"},
		Justification: "synthetic",
		Owner:         "maintainer",
		Expires:       toml.LocalDate{Year: 2030, Month: 2, Day: 1},
	}
	with := func(edit func(*Exception)) Exception {
		e := valid
		edit(&e)
		return e
	}
	finding := Finding{ID: "GHSA-aaaa-bbbb-cccc", Aliases: []string{"CVE-2030-1001"}, Artifact: "example.test/parser", Version: "v1.2.3", Blocking: true}
	scan := Scan{Scanner: "test", DBBuilt: now.Add(-day), Findings: []Finding{finding}}

	tests := []struct {
		name         string
		scan         Scan
		exceptions   []Exception
		wantBlocking int
		wantExcepted int
		wantFailure  string
	}{
		{name: "blocking finding fails", scan: scan, wantBlocking: 1},
		{name: "exception matches through an alias", scan: scan, exceptions: []Exception{with(func(e *Exception) { e.Aliases = []string{"CVE-2030-1001"} })}, wantExcepted: 1},
		{name: "exception for another advisory does not cover", scan: scan, exceptions: []Exception{valid}, wantBlocking: 1},
		{name: "exception for another version does not cover", scan: scan, exceptions: []Exception{with(func(e *Exception) { e.ID = "GHSA-aaaa-bbbb-cccc"; e.Versions = []string{"v1.2.2"} })}, wantBlocking: 1},
		{name: "exception for another artifact does not cover", scan: scan, exceptions: []Exception{with(func(e *Exception) { e.ID = "CVE-2030-1001"; e.Artifact = "example.test/other" })}, wantBlocking: 1},
		{name: "exception applies through its expiry day", scan: scan, exceptions: []Exception{with(func(e *Exception) { e.ID = "CVE-2030-1001"; e.Expires = toml.LocalDate{Year: 2030, Month: 1, Day: 15} })}, wantExcepted: 1},
		{name: "expired exception fails and stops covering", scan: scan, exceptions: []Exception{with(func(e *Exception) { e.ID = "CVE-2030-1001"; e.Expires = toml.LocalDate{Year: 2030, Month: 1, Day: 14} })}, wantBlocking: 1, wantFailure: "expired on 2030-01-14"},
		{name: "expired exception fails a clean scan", scan: Scan{DBBuilt: now}, exceptions: []Exception{with(func(e *Exception) { e.Expires = toml.LocalDate{Year: 2029, Month: 12, Day: 1} })}, wantFailure: "expired"},
		{name: "exception too far ahead fails", scan: Scan{DBBuilt: now}, exceptions: []Exception{with(func(e *Exception) { e.Expires = toml.LocalDate{Year: 2030, Month: 6, Day: 1} })}, wantFailure: "more than 90 days ahead"},
		{name: "exception missing fields fails", scan: Scan{DBBuilt: now}, exceptions: []Exception{{ID: "GO-2030-0001", Versions: []string{""}}}, wantFailure: "missing artifact; missing justification; missing owner; missing versions; missing expires"},
		{name: "stale database fails", scan: Scan{Scanner: "test", DBBuilt: now.Add(-3 * day)}, wantFailure: "test database was built 2030-01-12T12:00:00Z, 72h0m0s ago"},
		{name: "fresh database with no findings passes", scan: Scan{DBBuilt: now.Add(-day)}},
		{name: "non-blocking finding passes", scan: Scan{DBBuilt: now, Findings: []Finding{{ID: "GO-2030-0002"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Evaluate(tt.scan, tt.exceptions, 2*day, now)
			if len(res.Blocking) != tt.wantBlocking || len(res.Excepted) != tt.wantExcepted {
				t.Errorf("blocking=%d excepted=%d, want %d and %d", len(res.Blocking), len(res.Excepted), tt.wantBlocking, tt.wantExcepted)
			}
			failures := strings.Join(res.Failures, "\n")
			if tt.wantFailure == "" && failures != "" || !strings.Contains(failures, tt.wantFailure) {
				t.Errorf("failures = %q, want %q", failures, tt.wantFailure)
			}
			wantErr := tt.wantBlocking > 0 || tt.wantFailure != ""
			if err := res.Err(); errors.Is(err, ErrBlocked) != wantErr {
				t.Errorf("Err() = %v, want blocked=%v", err, wantErr)
			}
		})
	}
}

func TestWriteMarkdown(t *testing.T) {
	exceptions := loadFixtureExceptions(t)
	tests := []struct {
		golden  string
		parse   func(*os.File) (Scan, error)
		fixture string
	}{
		{"govulncheck-source.md", govulncheck, "govulncheck-source.json"},
		{"govulncheck-binary.md", govulncheck, "govulncheck-binary.json"},
		{"grype-image.md", grype, "grype-image.json"},
	}
	for _, tt := range tests {
		t.Run(tt.golden, func(t *testing.T) {
			res := Evaluate(parseFixture(t, tt.parse, tt.fixture), exceptions, 14*day, now)
			var buf bytes.Buffer
			if err := res.WriteMarkdown(&buf, tt.fixture); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", tt.golden)
			if *update {
				if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// Windows checkouts may convert the golden to CRLF.
			if got := buf.String(); got != strings.ReplaceAll(string(want), "\r\n", "\n") {
				t.Errorf("summary mismatch; rerun with -update and review:\n%s", got)
			}
		})
	}
}
