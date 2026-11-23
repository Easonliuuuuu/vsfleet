package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPIResources(t *testing.T) {
	db := filepath.Join(t.TempDir(), "history.db")
	t.Run("table", func(t *testing.T) {
		out, _, err := runCLI(t, db, "api-resources")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range [][]string{
			{"NAME", "SHORTNAMES", "SOURCE", "DESCRIPTION"},
			{"hosts", "esxi,node", "live,stored", "ESXi"},
			{"datastores", "ds", "live,stored"},
			{"events", "ev", "live", "vCenter"},
		} {
			if !hasRow(out, want) {
				t.Errorf("no row starting %q in:\n%s", want, out)
			}
		}
	})
	t.Run("json", func(t *testing.T) {
		out, _, err := runCLI(t, db, "api-resources", "-o", "json")
		if err != nil {
			t.Fatal(err)
		}
		var got []struct {
			Name       string   `json:"name"`
			Plural     string   `json:"plural"`
			ShortNames []string `json:"short_names"`
			Live       bool     `json:"live"`
			Stored     bool     `json:"stored"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("decoding %q: %v", out, err)
		}
		if len(got) == 0 || got[0].Name != "vm" || got[0].Plural != "vms" || !got[0].Live || !got[0].Stored {
			t.Fatalf("first kind = %+v; want vm/vms, live and stored", got)
		}
	})
}

// Removed CLI names must fail before a command can touch a vCenter or database.
func TestRemovedCommandsAreUnknown(t *testing.T) {
	for _, args := range [][]string{
		{"vm", "list"}, {"vms", "show", "web-01"},
		{"template", "list"}, {"host", "list"}, {"node", "list"},
		{"cluster", "list"}, {"vapp", "list"}, {"network", "list"},
		{"dvswitch", "list"}, {"dvportgroup", "list"},
		{"resourcepool", "list"}, {"snapshot", "list"},
		{"health"}, {"topology"}, {"dependencies"}, {"deps"},
		{"blast-radius"}, {"blast"}, {"search"},
		{"context", "test", "prod"}, {"contexts", "list"},
		{"portgroup", "list"}, {"portgroups", "list"}, {"status"},
		{"assessment", "run"}, {"assess", "list"}, {"history", "list"},
		{"import", "rvtools", "estate.xlsx"},
		{"compatibility", "report"}, {"compat", "report"},
		{"datastore", "list"}, {"datastore", "show", "ds1"},
		{"datastore", "files", "list", "ds1"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, _, err := runArgs(t, args...)
			name, parent := args[0], "vsfleet"
			if name == "datastore" {
				name, parent = args[1], "vsfleet datastore"
			}
			want := fmt.Sprintf("unknown command %q for %q", name, parent)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %v; want %q (output %q)", err, want, out)
			}
		})
	}
}

func TestRemovedHistoryDBFlagIsUnknown(t *testing.T) {
	_, _, err := runArgs(t, "--history-db", "x.db", "api-resources")
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --history-db") {
		t.Fatalf("error = %v; want unknown --history-db flag", err)
	}
}

// kubectl's mutating verbs are a habit worth answering: vsfleet is read-only,
// and the error says so rather than "unknown command".
func TestMutatingVerbsSayVsfleetIsReadOnly(t *testing.T) {
	for _, args := range [][]string{{"delete", "vm", "web-01"}, {"apply", "-f", "x.yaml"}, {"logs", "web-01"}} {
		t.Run(args[0], func(t *testing.T) {
			_, _, err := runArgs(t, args...)
			if err == nil || !strings.Contains(err.Error(), "read-only: vsfleet never changes a vCenter") {
				t.Fatalf("%v: error = %v; want a read-only refusal", args, err)
			}
		})
	}
}

// hasRow reports whether a line of a table begins with the given cells, in
// order, separated by any amount of space.
func hasRow(out string, cells []string) bool {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < len(cells) {
			continue
		}
		match := true
		for i, c := range cells {
			if fields[i] != c {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
