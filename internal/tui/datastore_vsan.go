package tui

import (
	"github.com/easonliuuuuu/vsfleet/internal/humanize"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// vsanReserveFields are a vSAN datastore's capacity reserves and the free
// space they leave for new disks; none for a datastore whose reserves were
// not read.
func vsanReserveFields(d vsphere.Datastore) []field {
	if d.VSANReserve == nil {
		return nil
	}
	return []field{
		{"vSAN reserves", d.VSANReserve.Summary()},
		{"Provisionable", humanize.Bytes(d.ProvisionableFreeBytes())},
	}
}
