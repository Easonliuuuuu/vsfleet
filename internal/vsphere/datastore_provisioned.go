package vsphere

// ProvisionedBytes is the space the datastore has promised: what is in use
// plus what its thin disks may still grow into. It is RVTools' vDatastore
// Provisioned MiB. ok is false when uncommitted space was not read or the
// datastore reports no capacity.
func (d Datastore) ProvisionedBytes() (bytes int64, ok bool) {
	if d.UncommittedBytes == nil || d.CapacityBytes <= 0 {
		return 0, false
	}
	return d.UsedBytes() + *d.UncommittedBytes, true
}

// ProvisionedPercent is ProvisionedBytes as a percentage of capacity; above
// 100 the datastore is overcommitted, and thin disks growing into what they
// were promised would fill it.
func (d Datastore) ProvisionedPercent() (float64, bool) {
	p, ok := d.ProvisionedBytes()
	if !ok {
		return 0, false
	}
	return float64(p) / float64(d.CapacityBytes) * 100, true
}

// Overcommitted reports a datastore whose provisioned space exceeds its
// capacity. The TUI datastore view and the datastore-overcommitted health
// rule both read it, so they never disagree.
func (d Datastore) Overcommitted() bool {
	p, ok := d.ProvisionedBytes()
	return ok && p > d.CapacityBytes
}
