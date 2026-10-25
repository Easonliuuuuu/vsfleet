package cli

import (
	"encoding/json"
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

// A short name works as a command alias wherever the registry lists it, and
// "portgroup" now names only standard-switch port groups under host.
func TestInventoryCommandsAcceptRegistryNames(t *testing.T) {
	root := NewRootCommand(&App{})
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"node", "list"}, "vsfleet host list"},
		{[]string{"cl", "list"}, "vsfleet cluster list"},
		{[]string{"net", "list"}, "vsfleet network list"},
		{[]string{"tmpl", "list"}, "vsfleet template list"},
		{[]string{"host", "pg", "list"}, "vsfleet host portgroup list"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			cmd, _, err := root.Find(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if cmd.CommandPath() != tc.want {
				t.Fatalf("resolved to %q; want %q", cmd.CommandPath(), tc.want)
			}
		})
	}
}

// "portgroup" used to be an alias of network. It now fails with the two
// commands that replace it instead of quietly listing networks.
func TestRemovedPortgroupAliasPointsAtReplacements(t *testing.T) {
	db := filepath.Join(t.TempDir(), "history.db")
	for _, args := range [][]string{{"portgroup", "list"}, {"portgroups", "list", "-o", "json"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, _, err := runCLI(t, db, args...)
			if err == nil {
				t.Fatalf("succeeded with output %q; want an error", out)
			}
			for _, want := range []string{"vsfleet network list", "vsfleet host portgroup list"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
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
