package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
)

// runCLI executes the command tree against the history database at dbPath,
// with buffers standing in for the terminal.
func runCLI(t *testing.T, dbPath string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	a := &App{HistoryPath: dbPath, Out: &out, Err: &errOut}
	root := NewRootCommand(a)
	root.SetArgs(append([]string{"--history-db", dbPath}, args...))
	defer func() { _ = a.Close(context.Background()) }()
	err = root.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

// seedHistory writes one finished run holding results to a new history
// database and returns its path. schema is the run's inventory schema
// version; empty records none, as a run from before versioning did.
func seedHistory(t *testing.T, when time.Time, schema string, results ...assessment.ContextResult) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "history.db")
	seedRun(t, dbPath, when, schema, results...)
	return dbPath
}

// seedRun appends one finished run holding results to the history database
// at dbPath, creating it if needed. Each context is saved a minute after
// when, which is the time stored ages are measured from, and the run
// finishes a minute later.
func seedRun(t *testing.T, dbPath string, when time.Time, schema string, results ...assessment.ContextResult) {
	t.Helper()
	s, err := assessment.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	contexts := make([]*config.Context, 0, len(results))
	for _, r := range results {
		contexts = append(contexts, &config.Context{Name: r.Name, Endpoint: "https://" + r.Name + ".example"})
	}
	ctx := context.Background()
	run, err := s.StartRunWithMetadata(ctx, "test", contexts, when, assessment.RunMetadata{InventorySchemaVersion: schema})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if err := s.SaveContext(ctx, run.ID, r, when.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.FinishRun(ctx, run.ID, when.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

// collections reports every collection kind as empty, except the kinds an
// override names.
func collections(overrides ...assessment.CollectionResult) []assessment.CollectionResult {
	out := make([]assessment.CollectionResult, 0, 9)
	for _, kind := range []string{"vm", "host", "cluster", "resourcepool", "vapp", "dvswitch", "datastore", "network", "snapshot"} {
		c := assessment.CollectionResult{Kind: kind, Status: "empty"}
		for _, o := range overrides {
			if o.Kind == kind {
				c = o
			}
		}
		out = append(out, c)
	}
	return out
}

// resourcePayload records value as a stored resource observation.
func resourcePayload(t *testing.T, context, vcenter, kind, id, name string, value any) assessment.ResourceObservation {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return assessment.ResourceObservation{Context: context, VCenterID: vcenter, Kind: kind, ID: id, Name: name, Payload: payload}
}
