// This file exercises "vsfleet get" end to end against vcsim: the kubectl
// argument forms, name globs, several kinds at once, and the errors for a
// name that matches nothing and a narrowing flag no kind takes.
package tests

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vmware/govmomi/simulator"
)

func TestGetArgumentForms(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 2
		m.Machine = 2
	})
	r := newRunner(t)
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")

	for _, tc := range []struct {
		name    string
		args    []string
		want    []string
		notWant []string
		stderr  string
		wantErr string
	}{
		{name: "KIND/NAME across kinds", args: []string{"vm/DC0_C0_RP0_VM0", "host/DC0_C0_H1"},
			want: []string{"DC0_C0_RP0_VM0", "DC0_C0_H1", "VERSION"}, notWant: []string{"DC0_C0_RP0_VM1"}},
		{name: "short name and glob", args: []string{"node", "*_h1"}, want: []string{"DC0_C0_H1"}, notWant: []string{"DC0_C0_H0"}},
		{name: "glob matching nothing", args: []string{"vms", "zzz*"}, stderr: "No resources found."},
		{name: "exact name matching nothing", args: []string{"vms", "DC0_C0_RP0_VM0", "no-such-vm"},
			want: []string{"DC0_C0_RP0_VM0"}, wantErr: `no vm matched "no-such-vm"`},
		{name: "narrowing flag no kind takes", args: []string{"vms", "--host", "DC0_C0_H0"}, wantErr: "--host applies to hbas"},
		{name: "events are not listed", args: []string{"events"}, wantErr: "vsfleet events --for"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := r.run(testPassword+"\n", append([]string{"get"}, tc.args...)...)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("get %v: %v\n%s", tc.args, err, stderr)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("get %v: error = %v; want %q", tc.args, err, tc.wantErr)
			}
			for _, want := range tc.want {
				if !strings.Contains(stdout, want) {
					t.Errorf("get %v: output missing %q:\n%s", tc.args, want, stdout)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(stdout, notWant) {
					t.Errorf("get %v: output has %q:\n%s", tc.args, notWant, stdout)
				}
			}
			if !strings.Contains(stderr, tc.stderr) {
				t.Errorf("get %v: stderr missing %q:\n%s", tc.args, tc.stderr, stderr)
			}
		})
	}

	// Several kinds as JSON are one object with an array per kind, so a
	// script can index by plural instead of guessing which array is which.
	var byKind map[string][]struct {
		Name string `json:"name"`
	}
	out := r.mustRun(testPassword+"\n", "get", "vms,hosts", "-o", "json")
	if err := json.Unmarshal([]byte(out), &byKind); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if len(byKind) != 2 || len(byKind["vms"]) == 0 || len(byKind["hosts"]) == 0 {
		t.Fatalf("get vms,hosts -o json = %+v", byKind)
	}
}
