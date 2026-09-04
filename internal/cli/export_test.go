package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestAssessmentExportIsOfflineAndNoClobbering(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "history.db")
	s, err := assessment.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	run, err := s.StartRun(context.Background(), "test", []*config.Context{{Name: "prod", Endpoint: "https://vc.example"}}, when)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveContext(context.Background(), run.ID, assessment.ContextResult{Name: "prod", VCenterID: "vc-uuid", Status: "success", VMs: []assessment.Observation{{Context: "prod", VCenterID: "vc-uuid", VM: vsphere.VM{ID: "vm-1", Name: "app"}}}}, when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishRun(context.Background(), run.ID, when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	firstPath := filepath.Join(dir, "first.xlsx")
	secondPath := filepath.Join(dir, "second.xlsx")
	var out, stderr bytes.Buffer
	a := &App{HistoryPath: dbPath, Out: &out, Err: &stderr}
	root := NewRootCommand(a)
	root.SetArgs([]string{"--history-db", dbPath, "assessment", "export", "latest", "--format", "rvtools", "--file", firstPath})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr.String(), "vCenter") {
		t.Fatalf("unexpected live connection warning: %s", stderr.String())
	}
	first, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}

	a = &App{HistoryPath: dbPath, Out: &out, Err: &stderr}
	root = NewRootCommand(a)
	root.SetArgs([]string{"--history-db", dbPath, "assessment", "export", "latest", "--file", secondPath})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("exports of one run differ")
	}

	a = &App{HistoryPath: dbPath, Out: &out, Err: &stderr}
	root = NewRootCommand(a)
	root.SetArgs([]string{"--history-db", dbPath, "assessment", "export", "latest", "--file", firstPath})
	if err := root.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("no-clobber error=%v", err)
	}
}
