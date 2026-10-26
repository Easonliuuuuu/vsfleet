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

// Every removed command, under any spelling the registry gives its kind,
// fails with the command that replaced it instead of "unknown command".
func TestRemovedCommandsPointAtReplacements(t *testing.T) {
	db := filepath.Join(t.TempDir(), "history.db")
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"vm", "list", "--filter", "web"}, []string{`"vm list" was renamed`, "vsfleet get vms"}},
		{[]string{"vms", "show", "web-01"}, []string{"vsfleet describe vm NAME"}},
		{[]string{"vm", "events", "web-01"}, []string{"vsfleet events --for vm/NAME"}},
		{[]string{"net", "list"}, []string{"vsfleet get networks"}},
		{[]string{"node", "list"}, []string{`"host list" was renamed`, "vsfleet get hosts"}},
		{[]string{"host", "show", "esxi-07"}, []string{"vsfleet describe host NAME"}},
		{[]string{"host", "pg", "list", "--host", "esxi-07"}, []string{"vsfleet get portgroups --host HOST"}},
		{[]string{"tmpl", "list"}, []string{"vsfleet get templates"}},
		{[]string{"snapshot", "list", "--vm", "web-01"}, []string{"vsfleet get snapshots"}},
		{[]string{"portgroup", "list"}, []string{"vsfleet get networks", "vsfleet get portgroups"}},
		{[]string{"portgroups", "list", "-o", "json"}, []string{"vsfleet get networks", "vsfleet get portgroups"}},
		{[]string{"status", "-A"}, []string{"vsfleet cluster-info"}},
		{[]string{"compatibility", "report", "--sheet", "vInfo"}, []string{"vsfleet explain rvtools.SHEET"}},
		{[]string{"assessment", "perf", "show", "latest"}, []string{`"assessment perf show" was renamed`, "vsfleet top vm"}},
		{[]string{"search", "ubuntu-golden", "-A"}, []string{`"search" was renamed`, "vsfleet get all '*TEXT*' -A"}},
		{[]string{"vm", "history", "web-01"}, []string{`"vm history" was renamed`, "vsfleet describe vm NAME --history"}},
		{[]string{"assessment", "inventory", "--kind", "host"}, []string{`"assessment inventory" was renamed`, "vsfleet get KIND --run RUN"}},
		{[]string{"health", "--severity", "warning"}, []string{`"health" was renamed`, "vsfleet analyze health"}},
		{[]string{"assessment", "findings", "nightly"}, []string{`"assessment findings" was renamed`, "vsfleet analyze health"}},
		{[]string{"assessment", "readiness"}, []string{"vsfleet analyze readiness"}},
		{[]string{"assessment", "network-readiness", "--source", "a", "--target", "b"}, []string{"vsfleet analyze readiness network cl/SOURCE cl/TARGET"}},
		{[]string{"network", "compare", "a", "b"}, []string{`"network compare" was renamed`, "vsfleet analyze readiness network"}},
		{[]string{"assessment", "sizing", "--hosts", "4"}, []string{"vsfleet analyze sizing"}},
		{[]string{"assessment", "capacity"}, []string{"vsfleet analyze capacity"}},
		{[]string{"assessment", "orphans"}, []string{"vsfleet analyze orphans"}},
		{[]string{"assessment", "snapshots", "--at", "4"}, []string{"vsfleet analyze snapshots"}},
		{[]string{"assessment", "trends", "churn"}, []string{`"assessment trends churn" was renamed`, "vsfleet analyze churn"}},
		{[]string{"assessment", "trends", "capacity", "--kind", "host"}, []string{"vsfleet analyze capacity --trend"}},
		{[]string{"topology", "vm", "web-01"}, []string{`"topology" was renamed`, "vsfleet analyze topology KIND/NAME"}},
		{[]string{"deps", "vm", "web-01"}, []string{"vsfleet analyze deps KIND/NAME"}},
		{[]string{"blast", "host", "esxi-07"}, []string{"vsfleet analyze blast-radius KIND/NAME"}},
		{[]string{"vm", "decommission-check", "web-01"}, []string{`"vm decommission-check" was renamed`, "vsfleet analyze decommission vm/NAME"}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			out, _, err := runCLI(t, db, tc.args...)
			if err == nil {
				t.Fatalf("succeeded with output %q; want an error", out)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
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
