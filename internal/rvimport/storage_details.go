package rvimport

import (
	"fmt"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// datastoreDetailHeaders are the vDatastore columns read into
// Datastore.Details.
var datastoreDetailHeaders = []string{"Config status", "Address", "SIOC enabled", "SIOC Threshold", "Hosts", "Cluster name", "Block size", "Max Blocks", "# Extents", "Major Version", "Version", "VMFS Upgradeable", "MHA"}

// applyDatastoreDetailCells reads vDatastore's configuration columns. A row
// with none of them, as in a workbook vsfleet exported before inventory
// schema 35, leaves Details nil. RVTools writes the VM counts but not which
// VMs, so VMIDs stay nil, "not read". A VMFS datastore's Address lists its
// extents, which also become its backing, so its LUNs' vMultiPath rows
// export again.
func applyDatastoreDetailCells(t sheetTable, row []string, d *vsphere.Datastore) {
	any := false
	for _, h := range datastoreDetailHeaders {
		if t.cell(row, h) != "" {
			any = true
		}
	}
	if !any {
		return
	}
	details := &vsphere.DatastoreDetails{
		ConfigStatus:       t.cell(row, "Config status"),
		Address:            t.cell(row, "Address"),
		Hosts:              splitList(t.cell(row, "Hosts")),
		MultipleHostAccess: parseOptBool(t.cell(row, "MHA")),
		SIOCEnabled:        parseOptBool(t.cell(row, "SIOC enabled")),
		SIOCThreshold:      parseOptInt32(t.cell(row, "SIOC Threshold")),
	}
	if strings.EqualFold(d.Type, "VMFS") {
		details.VMFS = &vsphere.DatastoreVMFS{
			BlockSizeMB:  int32(parseInt(t.cell(row, "Block size"))),
			MaxBlocks:    int32(parseInt(t.cell(row, "Max Blocks"))),
			Extents:      int(parseInt(t.cell(row, "# Extents"))),
			MajorVersion: int32(parseInt(t.cell(row, "Major Version"))),
			Version:      t.cell(row, "Version"),
			Upgradable:   parseBool(t.cell(row, "VMFS Upgradeable")),
		}
		if len(d.Backing.Extents) == 0 {
			d.Backing.Extents = splitList(details.Address)
		}
	}
	if name := t.cell(row, "Cluster name"); name != "" {
		details.StorageCluster = &vsphere.DatastoreCluster{Name: name, CapacityBytes: mibBytes(t.cell(row, "Cluster capacity MiB")), FreeBytes: mibBytes(t.cell(row, "Cluster free space MiB"))}
	}
	d.Details = details
}

// multipathDeviceFromRow reads an RVTools-layout vMultiPath row's LUN
// identity and paths, nil when the row carries none of them. Vendor, Model
// and Serial # keep their padding.
func multipathDeviceFromRow(t sheetTable, row []string) *vsphere.HostMultipathLUN {
	d := &vsphere.HostMultipathLUN{
		CanonicalName:    t.cell(row, "Disk"),
		OperationalState: t.cell(row, "Oper. State"),
		VStorageSupport:  t.cell(row, "vStorage"),
		QueueDepth:       parseOptInt32(t.cell(row, "Queue depth")),
		Vendor:           t.rawCell(row, "Vendor"),
		Model:            t.rawCell(row, "Model"),
		Revision:         t.cell(row, "Revision"),
		SCSILevel:        parseOptInt32(t.cell(row, "Level")),
		Serial:           t.rawCell(row, "Serial #"),
		UUID:             t.cell(row, "UUID"),
	}
	for i := 1; i <= 8; i++ {
		adapter := t.cell(row, fmt.Sprintf("Path %d", i))
		if adapter == "" {
			continue
		}
		// The workbook names the adapter, not the runtime name.
		d.Paths = append(d.Paths, vsphere.HostMultipathPath{Name: adapter, Adapter: adapter, State: t.cell(row, fmt.Sprintf("Path %d state", i))})
	}
	if d.CanonicalName == "" && d.OperationalState == "" && d.VStorageSupport == "" && d.Vendor == "" && d.UUID == "" && len(d.Paths) == 0 {
		return nil
	}
	return d
}

// rawCell is cell without trimming, for the SCSI inquiry strings whose
// padding is part of the value. A cell of only spaces is still empty.
func (t sheetTable) rawCell(row []string, header string) string {
	if t.cell(row, header) == "" {
		return ""
	}
	return row[t.index[canonicalHeader(t.name, header)]]
}
