package vsphere_test

import (
	"context"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestHostConfigIsExplicitlyOptIn(t *testing.T) {
	c, _ := newSimulator(t, nil)
	ctx := context.Background()
	idx, err := c.NewIndex(ctx)
	if err != nil {
		t.Fatalf("new index: %v", err)
	}

	summary := c.FetchGroupWith(ctx, idx, vsphere.GroupHosts, vsphere.FetchOptions{Detail: vsphere.DetailFull})
	if len(summary.Hosts) == 0 {
		t.Fatalf("summary host fetch returned no hosts")
	}
	for _, host := range summary.Hosts {
		if len(host.HBAs) != 0 || len(host.NICs) != 0 || len(host.VSwitches) != 0 || len(host.PortGroups) != 0 || len(host.VMKs) != 0 || len(host.Multipaths) != 0 {
			t.Fatalf("host %q fetched config without HostConfig option: %+v", host.Name, host)
		}
	}

	full := c.FetchGroupWith(ctx, idx, vsphere.GroupHosts, vsphere.FetchOptions{Detail: vsphere.DetailFull, HostConfig: true})
	if len(full.Hosts) != len(summary.Hosts) {
		t.Fatalf("full host fetch returned %d hosts, summary returned %d", len(full.Hosts), len(summary.Hosts))
	}
	var found bool
	for _, host := range full.Hosts {
		if len(host.HBAs) > 0 && len(host.NICs) > 0 && len(host.VSwitches) > 0 && len(host.PortGroups) > 0 && len(host.VMKs) > 0 && len(host.Multipaths) > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("full host fetch returned no complete host configuration: %+v", full.Hosts)
	}
}
