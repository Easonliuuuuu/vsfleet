// This file exercises issue #158's host storage/network subresource
// commands ("host hba list" and its siblings) end to end against a
// simulated vCenter: the evidence already collected for assessments must be
// queryable directly, scoped to one host on request, and never leak another
// host's devices.
package tests

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vmware/govmomi/simulator"
)

func TestHostSubresourceCommandsListDeviceEvidence(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 2
	})
	r := newRunner(t)
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")

	for _, tc := range []struct {
		name    string
		args    []string
		headers []string
	}{
		{"hba", []string{"host", "hba", "list"}, []string{"HOST", "DEVICE"}},
		{"pnic", []string{"host", "pnic", "list"}, []string{"HOST", "DEVICE"}},
		{"vswitch", []string{"host", "vswitch", "list"}, []string{"HOST", "NAME"}},
		{"portgroup", []string{"host", "portgroup", "list"}, []string{"HOST", "NAME"}},
		{"vmkernel", []string{"host", "vmkernel", "list"}, []string{"HOST", "DEVICE"}},
		{"multipath", []string{"host", "multipath", "list"}, []string{"HOST", "LUN"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := r.mustRun(testPassword+"\n", tc.args...)
			for _, header := range tc.headers {
				if !strings.Contains(out, header) {
					t.Errorf("%v is missing the %q column:\n%s", tc.args, header, out)
				}
			}
			if !strings.Contains(out, "DC0_C0_H0") && !strings.Contains(out, "DC0_C0_H1") {
				t.Errorf("%v produced no rows for either simulated host:\n%s", tc.args, out)
			}
		})
	}
}

func TestHostSubresourceHostFlagNarrowsAndNeverLeaks(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 2
	})
	r := newRunner(t)
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")

	out := r.mustRun(testPassword+"\n", "host", "vswitch", "list", "--host", "DC0_C0_H0", "-o", "json")
	var rows []struct {
		Host string `json:"host"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(rows) == 0 {
		t.Fatalf("--host filter returned no rows:\n%s", out)
	}
	for _, row := range rows {
		if row.Host != "DC0_C0_H0" {
			t.Errorf("--host DC0_C0_H0 leaked a row for %q:\n%s", row.Host, out)
		}
	}

	_, _, err := r.run(testPassword+"\n", "host", "vswitch", "list", "--host", "no-such-host")
	if err == nil {
		t.Fatal("an unknown --host should fail")
	}
	if !strings.Contains(err.Error(), "no-such-host") {
		t.Errorf("error should name the host, got: %v", err)
	}
}
