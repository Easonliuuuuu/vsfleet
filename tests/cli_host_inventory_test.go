package tests

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"

	"github.com/vmware/govmomi/simulator"
)

func TestAssessmentCaptureExportsHostStorageAndNetworkSheets(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 2
		m.Machine = 2
	})
	r := newRunner(t)
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")
	historyDB := filepath.Join(t.TempDir(), "history.db")

	if _, _, err := r.run("", "--history-db", historyDB, "assessment", "run", "--all-contexts"); err != nil {
		t.Fatalf("assessment run: %v", err)
	}

	firstDir := filepath.Join(t.TempDir(), "csv-1")
	r.mustRun("", "--history-db", historyDB, "assessment", "export", "latest", "--format", "csv", "--file", firstDir)
	secondDir := filepath.Join(t.TempDir(), "csv-2")
	r.mustRun("", "--history-db", historyDB, "assessment", "export", "latest", "--format", "csv", "--file", secondDir)

	for _, name := range []string{"vHBA.csv", "vSwitch.csv", "dvSwitch.csv", "dvPort.csv"} {
		first, err := os.ReadFile(filepath.Join(firstDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		second, err := os.ReadFile(filepath.Join(secondDir, name))
		if err != nil {
			t.Fatalf("read second %s: %v", name, err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("repeated %s exports differ", name)
		}
		rows, err := csv.NewReader(bytes.NewReader(first)).ReadAll()
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if len(rows) < 2 {
			t.Fatalf("%s has no captured rows", name)
		}
	}

	coverageBytes, err := os.ReadFile(filepath.Join(firstDir, "vsfleetCoverage.csv"))
	if err != nil {
		t.Fatalf("read coverage: %v", err)
	}
	coverage, err := csv.NewReader(bytes.NewReader(coverageBytes)).ReadAll()
	if err != nil {
		t.Fatalf("parse coverage: %v", err)
	}
	for _, wantSheet := range []string{"vHBA", "vSwitch", "dvSwitch", "dvPort"} {
		found := false
		for _, row := range coverage[1:] {
			if len(row) >= 13 && row[9] == wantSheet {
				found = true
				if row[10] != "success" || row[11] == "0" {
					t.Fatalf("coverage for %s = status %q, count %q", wantSheet, row[10], row[11])
				}
			}
		}
		if !found {
			t.Fatalf("coverage did not include %s", wantSheet)
		}
	}
}
