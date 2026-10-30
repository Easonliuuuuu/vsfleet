package cli

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// Every one of these is a mistake in the command line itself, so it fails
// before configuration is read or a vCenter is asked, and says what to do.
func TestGetFlagMistakesFailFast(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"analyze", "health", "-o", "yaml"}, `-o yaml is not supported by "vsfleet analyze health", which takes table, wide or json`},
		{[]string{"get", "vms", "-o", "xml"}, `unknown output format "xml"`},
		{[]string{"get", "vms", "-o", "jsonpath"}, "-o jsonpath needs a template"},
		{[]string{"get", "vms", "-o", "yaml=x"}, "-o yaml takes no argument"},
		{[]string{"get", "vms", "-o", "jsonpath={.items[*]"}, "unclosed {"},
		{[]string{"get", "vms", "-o", "custom-columns=NAME"}, "is not NAME:EXPRESSION"},
		{[]string{"get", "vms", "-l", "env in prod"}, "go in parentheses"},
		{[]string{"get", "vms", "--sort-by", "name"}, "must start with"},
		{[]string{"get", "vms", "--refresh", "1m"}, "--refresh applies with -w"},
		{[]string{"get", "vms", "--where", "cpu>=8"}, "--where was renamed"},
		{[]string{"get", "vms", "--wide"}, "use -o wide"},
		{[]string{"get", "vms", "-w", "--run", "latest"}, "a capture --run names never changes"},
		{[]string{"describe", "vm", "web-01", "--history", "--run", "latest"}, "--history reads every capture; --run picks one"},
		{[]string{"describe", "vm", "web-01", "--include-runtime"}, "apply with --history"},
		{[]string{"describe", "host", "esxi-07", "--history"}, "--history describes one VM at a time"},
		{[]string{"describe", "all", "web-01"}, `"all" lists with get`},
		{[]string{"events", "--for", "vm/web-01", "--run", "latest"}, "events are read live"},
		{[]string{"top", "vm", "--run", "latest"}, "pick one with --window"},
		{[]string{"top"}, `top needs a kind: "vsfleet top vm [NAME...]"`},
		{[]string{"analyze", "health", "--wide"}, "--wide was renamed: use -o wide"},
		{[]string{"analyze", "health", "--where", "cpu>=8"}, "--where was renamed"},
		{[]string{"analyze", "readiness", "-o", "wide"}, "which takes table or json"},
		{[]string{"analyze", "capacity", "--kind", "host"}, "--kind applies with --trend"},
		{[]string{"analyze", "capacity", "--trend", "--min-free", "20"}, "--min-free applies to the projection, not --trend"},
		{[]string{"analyze", "snapshots", "--from", "1"}, "--from applies with --trend"},
		{[]string{"analyze", "snapshots", "--at", "1"}, "--at was renamed: use --run"},
		{[]string{"analyze", "churn", "--to", "1"}, "--to was renamed: use --run"},
		{[]string{"analyze", "topology", "vm", "web-01", "nightly"}, "select the capture with --run"},
		{[]string{"analyze", "decommission", "host/esxi-07"}, "decommission reviews VMs"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			_, _, err := runArgs(t, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v; want %q", err, tc.want)
			}
		})
	}
}

// An empty answer says "No resources found."; a name that matched nothing is
// an error, and says only that.
func TestGetEmptyAndMissingAreReportedOnce(t *testing.T) {
	dbPath := newExportTestHistoryDB(t)
	for _, tc := range []struct {
		args      []string
		wantErr   string
		wantEmpty bool
	}{
		{args: []string{"get", "vm", "nope", "--run", "latest"}, wantErr: `no vm matched "nope"`},
		{args: []string{"get", "vms", "--field-selector", "name=nope", "--run", "latest"}, wantEmpty: true},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			stdout, stderr, err := runCLI(t, dbPath, tc.args...)
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("error = %v; want %q", err, tc.wantErr)
			}
			if got := strings.Contains(stderr, "No resources found."); got != tc.wantEmpty || stdout != "" {
				t.Fatalf("stdout = %q, stderr = %q; want \"No resources found.\" only for an empty answer", stdout, stderr)
			}
		})
	}
}

// vSphere reports a distributed switch without a port limit as the largest
// int32; the table says so in words.
func TestGetDVSwitchesReadsNoPortLimitAsUnlimited(t *testing.T) {
	dvs := vsphere.DVSwitch{Location: vsphere.Location{Context: "prod"}, ID: "dvs-1", Name: "dvs-a", NumPorts: 388, MaxPorts: math.MaxInt32}
	dbPath := seedHistory(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), "", assessment.ContextResult{Name: "prod", VCenterID: "vc-1", Status: "success", Collections: collections(
		assessment.CollectionResult{Kind: "dvswitch", Status: "success", ItemCount: 1, Resources: []assessment.ResourceObservation{resourcePayload(t, "prod", "vc-1", "dvswitch", dvs.ID, dvs.Name, dvs)}},
	)})
	out := mustRunCLI(t, dbPath, "get", "dvswitches", "--run", "latest")
	if !strings.Contains(out, "unlimited") || strings.Contains(out, "2147483647") {
		t.Fatalf("get dvswitches = %q; want MAX PORTS unlimited", out)
	}
}

// --run latest and previous pick among the captures of the contexts -n
// names, so a newer capture of another vCenter does not hide them.
func TestGetRunLatestFollowsTheSelectedContext(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	observe := func(contextName, vcenter, name string) assessment.ContextResult {
		vm := vsphere.VM{Location: vsphere.Location{Context: contextName}, ID: "vm-1", Name: name, InstanceUUID: name}
		return assessment.ContextResult{Name: contextName, VCenterID: vcenter, Status: "success", VMs: []assessment.Observation{{VCenterID: vcenter, Context: contextName, VM: vm}}}
	}
	dbPath := seedHistory(t, when, "", observe("prod", "vc-1", "web-01"))
	seedRun(t, dbPath, when.Add(time.Hour), "", observe("dr", "vc-2", "db-01"))

	out := mustRunCLI(t, dbPath, "get", "vms", "-n", "prod", "--run", "latest")
	if !strings.Contains(out, "web-01") || strings.Contains(out, "db-01") {
		t.Fatalf("get vms -n prod --run latest:\n%s\nwant prod's capture", out)
	}
	if _, _, err := runCLI(t, dbPath, "get", "vms", "-n", "prod", "--run", "previous"); err == nil || !strings.Contains(err.Error(), "no previous capture of prod") {
		t.Fatalf("get vms -n prod --run previous: err = %v; want no previous capture of prod", err)
	}
}

// A capture that recorded a kind as partial says so when read, rather than
// presenting what it did return as the whole answer. Each gap is reported,
// however many kinds a context has one for.
func TestGetRunReportsAPartialCollection(t *testing.T) {
	const reason = `possible permission-hidden inventory: VM "app" uses network`
	dbPath := seedHistory(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), "", assessment.ContextResult{Name: "prod", VCenterID: "vc-1", Status: "success", Collections: collections(
		assessment.CollectionResult{Kind: "network", Status: assessment.CollectionPartial, Error: reason},
		assessment.CollectionResult{Kind: "dvswitch", Status: assessment.CollectionPartial, Error: "possible permission-hidden inventory: VM \"app\" uses distributed switch 50 aa"},
	)})
	stdout, stderr, err := runCLI(t, dbPath, "get", "networks", "--run", "latest")
	if err != nil || stdout != "" {
		t.Fatalf("get networks: stdout = %q, err = %v; want a warning and exit 0", stdout, err)
	}
	if !strings.Contains(stderr, reason) || strings.Contains(stderr, "No resources found.") {
		t.Fatalf("get networks: stderr = %q; want the gap instead of an empty answer", stderr)
	}
	_, stderr, err = runCLI(t, dbPath, "get", "networks,dvswitches", "--run", "latest")
	if err != nil || !strings.Contains(stderr, "uses network") || !strings.Contains(stderr, "uses distributed switch") {
		t.Fatalf("get networks,dvswitches: err = %v, stderr = %q; want both gaps", err, stderr)
	}
}
