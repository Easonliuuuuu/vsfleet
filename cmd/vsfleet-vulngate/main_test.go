package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fixtures = "../../internal/vulngate/testdata"

func TestRunExitCodes(t *testing.T) {
	dir := t.TempDir()
	noExceptions := filepath.Join(dir, "none.toml")
	if err := os.WriteFile(noExceptions, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2030, 1, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		args       []string
		now        time.Time
		want       int
		wantStdout string
	}{
		{"binary scan without blocking findings passes", []string{"-format", "govulncheck", "-exceptions", noExceptions, fixtures + "/govulncheck-binary.json"}, now, 0, "passed"},
		{"reachable finding fails", []string{"-format", "govulncheck", "-exceptions", noExceptions, fixtures + "/govulncheck-source.json"}, now, 1, "Blocking findings"},
		{"fixable critical fails", []string{"-format", "grype", "-exceptions", noExceptions, fixtures + "/grype-image.json"}, now, 1, "fixable Critical"},
		{"expired exception fails a clean scan", []string{"-format", "govulncheck", "-exceptions", fixtures + "/exceptions.toml", fixtures + "/govulncheck-binary.json"}, now, 1, "expired on 2030-01-01"},
		{"default database age applies per scanner", []string{"-format", "grype", "-exceptions", noExceptions, fixtures + "/grype-image.json"}, now.Add(6 * 24 * time.Hour), 1, "limit 120h0m0s"},
		{"database outage is not a clean scan", []string{"-format", "grype", "-exceptions", noExceptions, fixtures + "/grype-invalid-db.json"}, now, 2, "produced no usable report"},
		{"missing report", []string{"-format", "grype", "-exceptions", noExceptions, filepath.Join(dir, "absent.json")}, now, 2, "produced no usable report"},
		{"missing exception file", []string{"-format", "govulncheck", "-exceptions", filepath.Join(dir, "absent.toml"), fixtures + "/govulncheck-binary.json"}, now, 2, ""},
		{"unknown format", []string{"-format", "trivy", fixtures + "/grype-image.json"}, now, 2, ""},
		{"no report argument", []string{"-format", "grype"}, now, 2, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run(tt.args, &stdout, &stderr, tt.now); got != tt.want {
				t.Errorf("exit = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, tt.want, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout lacks %q:\n%s", tt.wantStdout, stdout.String())
			}
		})
	}
}
