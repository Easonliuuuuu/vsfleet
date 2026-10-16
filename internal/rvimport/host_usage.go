package rvimport

import (
	"math"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// applyHostUsage turns vHost's CPU usage % and Memory usage % back into the
// MHz and MiB vsfleet stores. RVTools writes whole percentages, so the result
// is accurate to about one percent of capacity. Without it an imported host
// with real capacity would read as idle.
func applyHostUsage(t sheetTable, row []string, host *vsphere.Host) {
	if pct, ok := parseNumber(t.cell(row, "CPU usage %")); ok {
		host.CPUUsageMHz = int64(math.Round(pct / 100 * float64(host.TotalCPU())))
	}
	if pct, ok := parseNumber(t.cell(row, "Memory usage %")); ok {
		host.MemoryUsageMB = int64(math.Round(pct / 100 * float64(host.MemoryMB)))
	}
}
