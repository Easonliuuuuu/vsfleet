package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// newExportTestHistoryDB persists one finished run with a single VM and
// returns the path to the history database, ready for `assessment export`.
func newExportTestHistoryDB(t *testing.T) string {
	t.Helper()
	return seedHistory(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), "", assessment.ContextResult{Name: "prod", VCenterID: "vc-uuid", Status: "success", VMs: []assessment.Observation{{Context: "prod", VCenterID: "vc-uuid", VM: vsphere.VM{ID: "vm-1", Name: "app"}}}})
}

func runExport(t *testing.T, dbPath string, args ...string) (stdout, stderr string) {
	t.Helper()
	stdout, stderr, err := runCLI(t, dbPath, append([]string{"assessment", "export"}, args...)...)
	if err != nil {
		t.Fatalf("export failed: %v (stderr=%s)", err, stderr)
	}
	return stdout, stderr
}

func TestAssessmentExportIsOfflineAndNoClobbering(t *testing.T) {
	dir := t.TempDir()
	dbPath := newExportTestHistoryDB(t)

	firstPath := filepath.Join(dir, "first.xlsx")
	secondPath := filepath.Join(dir, "second.xlsx")
	_, stderr := runExport(t, dbPath, "latest", "--format", "rvtools", "--file", firstPath)
	if strings.Contains(stderr, "vCenter") {
		t.Fatalf("unexpected live connection warning: %s", stderr)
	}
	first, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}

	runExport(t, dbPath, "latest", "--file", secondPath)
	second, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("exports of one run differ")
	}

	if _, _, err := runCLI(t, dbPath, "assessment", "export", "latest", "--file", firstPath); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("no-clobber error=%v", err)
	}
}

func TestAssessmentExportCSVIsOfflineDeterministicAndNoClobbering(t *testing.T) {
	dir := t.TempDir()
	dbPath := newExportTestHistoryDB(t)

	firstDir := filepath.Join(dir, "first-csv")
	secondDir := filepath.Join(dir, "second-csv")
	stdout, stderr := runExport(t, dbPath, "latest", "--format", "csv", "--file", firstDir)
	if strings.Contains(stderr, "vCenter") {
		t.Fatalf("unexpected live connection warning: %s", stderr)
	}
	if !strings.Contains(stdout, "vInfo.csv") {
		t.Fatalf("stdout missing per-file summary: %s", stdout)
	}

	runExport(t, dbPath, "latest", "--format", "csv", "--file", secondDir)

	firstFiles := readDirSorted(t, firstDir)
	secondFiles := readDirSorted(t, secondDir)
	if len(firstFiles) == 0 {
		t.Fatal("no CSV files were written")
	}
	if !equalStrings(firstFiles, secondFiles) {
		t.Fatalf("file lists differ: %v vs %v", firstFiles, secondFiles)
	}
	for _, name := range firstFiles {
		a, err := os.ReadFile(filepath.Join(firstDir, name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(secondDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("%s differs between exports", name)
		}
	}

	if _, _, err := runCLI(t, dbPath, "assessment", "export", "latest", "--format", "csv", "--file", firstDir); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("no-clobber error=%v", err)
	}

	// --force replaces the existing directory's files without error.
	runExport(t, dbPath, "latest", "--format", "csv", "--file", firstDir, "--force")
}

func TestAssessmentExportRejectsMismatchedExtensions(t *testing.T) {
	dir := t.TempDir()
	dbPath := newExportTestHistoryDB(t)

	if _, _, err := runCLI(t, dbPath, "assessment", "export", "latest", "--format", "csv", "--file", filepath.Join(dir, "out.xlsx")); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("csv with .xlsx path error=%v", err)
	}
	if _, _, err := runCLI(t, dbPath, "assessment", "export", "latest", "--format", "rvtools", "--file", filepath.Join(dir, "out.csv")); err == nil || !strings.Contains(err.Error(), ".xlsx extension") {
		t.Fatalf("rvtools without .xlsx error=%v", err)
	}
	if _, _, err := runCLI(t, dbPath, "assessment", "export", "latest", "--format", "yaml", "--file", filepath.Join(dir, "out.yaml")); err == nil || !strings.Contains(err.Error(), "unsupported export format") {
		t.Fatalf("unknown format error=%v", err)
	}
}

func readDirSorted(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
