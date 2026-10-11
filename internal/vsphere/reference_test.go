package vsphere_test

import (
	"context"
	"testing"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/simulator"
)

// TestNetworkDeletedOnlyForMissingObjects covers the answers that must not
// read as deleted: an object that exists, and one the account may not read.
// Calling either deleted would hide permission-hidden inventory. The deleted
// answer itself is covered end to end by the capture test for issue #403.
func TestNetworkDeletedOnlyForMissingObjects(t *testing.T) {
	tests := []struct {
		name    string
		denied  bool
		wantErr bool
	}{
		{name: "existing port group"},
		{name: "read denied", denied: true, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, faults := newSimulator(t, func(m *simulator.Model) { m.Portgroup = 1 })
			ctx := context.Background()
			network, err := find.NewFinder(c.VIM()).Network(ctx, "DC0_DVPG0")
			if err != nil {
				t.Fatal(err)
			}
			if tc.denied {
				faults.AddRule(&simulator.FaultInjectionRule{
					MethodName:  "RetrievePropertiesEx",
					ObjectType:  "*",
					ObjectName:  "*",
					Probability: 1,
					Enabled:     true,
					FaultType:   simulator.FaultTypeNoPermission,
					Message:     "Permission to perform this operation was denied.",
				})
			}

			deleted, err := c.NetworkDeleted(ctx, network.Reference().Value)
			if deleted || (err != nil) != tc.wantErr {
				t.Fatalf("NetworkDeleted = %v, %v; want false with error=%v", deleted, err, tc.wantErr)
			}
		})
	}
}
