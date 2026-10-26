// This file exercises "vsfleet get" output and selector flags end to end
// against vcsim (#501): -o wide, yaml, name, csv, jsonpath and
// custom-columns, -l over vSphere tags, --field-selector, --sort-by,
// --no-headers and -w.
package tests

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vapi/rest"
	_ "github.com/vmware/govmomi/vapi/simulator"
	"github.com/vmware/govmomi/vapi/tags"
)

// tagSimulatorVMs attaches category/name tags to VMs, creating the category
// and tags as needed. byVM maps a VM inventory path to "category=name".
func tagSimulatorVMs(t *testing.T, vc *vcenter, byVM map[string]string) {
	t.Helper()
	ctx := context.Background()
	client, err := govmomi.NewClient(ctx, mustURL(t, vc.URL), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Login(ctx, url.UserPassword("user", "pass")); err != nil {
		t.Fatal(err)
	}
	rc := rest.NewClient(client.Client)
	if err := rc.Login(ctx, url.UserPassword("user", "pass")); err != nil {
		t.Fatal(err)
	}
	m := tags.NewManager(rc)
	categories := map[string]string{}
	tagIDs := map[string]string{}
	finder := find.NewFinder(client.Client, false)
	for vmPath, label := range byVM {
		category, name, _ := strings.Cut(label, "=")
		if categories[category] == "" {
			id, err := m.CreateCategory(ctx, &tags.Category{Name: category, Cardinality: "MULTIPLE", AssociableTypes: []string{"VirtualMachine"}})
			if err != nil {
				t.Fatal(err)
			}
			categories[category] = id
		}
		if tagIDs[label] == "" {
			id, err := m.CreateTag(ctx, &tags.Tag{Name: name, CategoryID: categories[category]})
			if err != nil {
				t.Fatal(err)
			}
			tagIDs[label] = id
		}
		vm, err := finder.VirtualMachine(ctx, vmPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.AttachTag(ctx, tagIDs[label], vm.Reference()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGetOutputAndSelectors(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenterWithTagging(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 2
		m.Machine = 2
	})
	tagSimulatorVMs(t, vc, map[string]string{
		"/DC0/vm/DC0_C0_RP0_VM0": "env=prod",
		"/DC0/vm/DC0_C0_RP0_VM1": "env=dev",
	})
	r := newRunner(t)
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")

	for _, tc := range []struct {
		name    string
		args    []string
		want    string // exact stdout, when set
		has     []string
		hasNot  []string
		wantErr string
	}{
		{name: "wide adds tags", args: []string{"vms", "-o", "wide"}, has: []string{"TAGS", "env/prod", "env/dev"}},
		{name: "label equals, as names", args: []string{"vms", "-l", "env=prod", "-o", "name"}, want: "vm/DC0_C0_RP0_VM0\n"},
		{name: "label set", args: []string{"vms", "-l", "env in (prod,dev)", "-o", "name", "--sort-by", ".name"},
			want: "vm/DC0_C0_RP0_VM0\nvm/DC0_C0_RP0_VM1\n"},
		{name: "label absent", args: []string{"vms", "-l", "!env", "-o", "name"}, has: []string{"vm/DC0_H0_VM0"}, hasNot: []string{"DC0_C0_RP0"}},
		{name: "field selector", args: []string{"vms", "--field-selector", "name==DC0_C0_RP0_VM1", "-o", "name"}, want: "vm/DC0_C0_RP0_VM1\n"},
		{name: "tags belong to -l", args: []string{"vms", "--field-selector", "tag.env=prod"}, wantErr: "-l env=prod"},
		{name: "yaml keeps field order", args: []string{"vm/DC0_C0_RP0_VM0", "-o", "yaml"}, has: []string{"- context: lab\n  datacenter: DC0\n  path: /DC0/vm/DC0_C0_RP0_VM0\n", "name: DC0_C0_RP0_VM0"}},
		{name: "csv", args: []string{"hosts", "DC0_H0", "-o", "csv"}, want: "NAME,CLUSTER,STATE,CPU,RAM,VMS,VERSION\nDC0_H0,-,connected,2 cores,4.0G,2,8.0.2\n"},
		{name: "csv without headers", args: []string{"hosts", "DC0_H0", "-o", "csv", "--no-headers"}, want: "DC0_H0,-,connected,2 cores,4.0G,2,8.0.2\n"},
		{name: "csv is one kind", args: []string{"vms,hosts", "-o", "csv"}, wantErr: "-o csv takes one kind"},
		{name: "jsonpath on a named object", args: []string{"vm/DC0_C0_RP0_VM0", "-o", "jsonpath={.name} {.power_state}"}, want: "DC0_C0_RP0_VM0 poweredOn"},
		{name: "jsonpath on a list", args: []string{"vms", "DC0_C0_RP0_*", "-o", `jsonpath={range .items[*]}{.name}{"\n"}{end}`, "--sort-by", ".name"},
			want: "DC0_C0_RP0_VM0\nDC0_C0_RP0_VM1\n"},
		{name: "custom columns", args: []string{"vms", "-l", "env", "-o", "custom-columns=VM:.name,TAG:.metadata.tags[*].name,NONE:.nope", "--sort-by", ".name"},
			has: []string{"VM", "DC0_C0_RP0_VM0   prod   <none>", "DC0_C0_RP0_VM1   dev    <none>"}},
		{name: "removed --where", args: []string{"vms", "--where", "cpu>=1"}, wantErr: "--field-selector"},
		{name: "removed --wide", args: []string{"vms", "--wide"}, wantErr: "use -o wide"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := r.run(testPassword+"\n", append([]string{"get"}, tc.args...)...)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("get %v: error = %v; want %q", tc.args, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("get %v: %v\n%s", tc.args, err, stderr)
			}
			if tc.want != "" && stdout != tc.want {
				t.Fatalf("get %v:\n%q\nwant\n%q", tc.args, stdout, tc.want)
			}
			for _, s := range tc.has {
				if !strings.Contains(stdout, s) {
					t.Errorf("get %v: output missing %q:\n%s", tc.args, s, stdout)
				}
			}
			for _, s := range tc.hasNot {
				if strings.Contains(stdout, s) {
					t.Errorf("get %v: output has %q:\n%s", tc.args, s, stdout)
				}
			}
		})
	}
}

// -w renders, waits the refresh interval and renders again, until the
// context ends; ending it is a clean exit, not an error.
func TestGetWatchRerendersUntilInterrupted(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, nil)
	r := newRunner(t)
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stdout, stderr, err := r.runContext(ctx, testPassword+"\n", "get", "hosts", "-w", "--refresh", "200ms", "-o", "name")
	if err != nil {
		t.Fatalf("get -w: %v\n%s", err, stderr)
	}
	if renders := strings.Count(stdout, "host/DC0_H0\n"); renders < 2 {
		t.Fatalf("get -w rendered %d times in 2s at 200ms:\n%s", renders, stdout)
	}
}
