package vsphere_test

import (
	"context"
	"testing"
)

// Every accessible datastore records its uncommitted space, so provisioned
// space and overcommit can be judged; the amount is the simulator's.
func TestAccessibleDatastoresRecordUncommittedSpace(t *testing.T) {
	c, _ := newSimulator(t, nil)
	datastores, err := c.ListDatastores(context.Background())
	if err != nil {
		t.Fatalf("list datastores: %v", err)
	}
	accessible := 0
	for _, ds := range datastores {
		if !ds.Accessible {
			continue
		}
		accessible++
		if ds.UncommittedBytes == nil {
			t.Errorf("%s: uncommitted space was not recorded", ds.Name)
		}
		if _, ok := ds.ProvisionedBytes(); !ok {
			t.Errorf("%s: provisioned space unknown", ds.Name)
		}
	}
	if accessible == 0 {
		t.Fatal("the simulator listed no accessible datastore")
	}
}
