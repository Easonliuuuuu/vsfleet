package rvimport

import "github.com/easonliuuuuu/vsfleet/internal/vsphere"

// datastoreUncommitted recovers a datastore's uncommitted space from
// vDatastore's Provisioned MiB, which RVTools writes as in-use plus
// uncommitted space. An empty cell is not read. RVTools rounds to whole MiB,
// so a result just below zero is rounding and reads as zero.
func datastoreUncommitted(d vsphere.Datastore, provisionedMiB string) *int64 {
	p := mibBytes(provisionedMiB)
	if p == nil {
		return nil
	}
	u := max(*p-d.UsedBytes(), 0)
	return &u
}
