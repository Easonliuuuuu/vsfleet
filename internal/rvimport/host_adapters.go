package rvimport

import "github.com/easonliuuuuu/vsfleet/internal/vsphere"

// hostAdapterCounts are vHost's # NICs and # HBAs. The import stores no count
// of its own: the vNIC and vHBA rows rebuild each host's adapters, and the
// counts only check that every row was attached.
type hostAdapterCounts struct {
	host       *vsphere.Host
	nics, hbas *int64
}

func adapterCountsFromRow(t sheetTable, row []string, h *vsphere.Host) hostAdapterCounts {
	return hostAdapterCounts{host: h, nics: parseOptInt64(t.cell(row, "# NICs")), hbas: parseOptInt64(t.cell(row, "# HBAs"))}
}

// checkHostAdapterCounts warns when the vNIC or vHBA rows attached to a host
// disagree with its vHost count, as when rows were skipped because their host
// could not be identified. A worksheet the workbook lacks is already a gap.
func checkHostAdapterCounts(tables map[string]sheetTable, counts []hostAdapterCounts, warn func(string, ...any)) {
	check := func(h *vsphere.Host, sheet, column string, want *int64, got int) {
		if _, ok := tables[sheet]; !ok || want == nil || *want == int64(got) {
			return
		}
		warn("%s: host %q has %s %d on %s, but %d attached", sheet, h.Name, column, *want, sheetVHost, got)
	}
	for _, c := range counts {
		check(c.host, sheetVNIC, "# NICs", c.nics, len(c.host.NICs))
		check(c.host, sheetVHBA, "# HBAs", c.hbas, len(c.host.HBAs))
	}
}
