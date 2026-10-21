package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// TestClusterMemoryUsesOnePrecision checks that a cluster's used and total
// memory are written to the same precision (issue #475).
func TestClusterMemoryUsesOnePrecision(t *testing.T) {
	c := vsphere.Cluster{Name: "compute", TotalCPUMHz: 20000, TotalMemoryMB: 6464 * 1024}
	members := clusterMembers{hosts: []vsphere.Host{{CPUUsageMHz: 16000, MemoryUsageMB: 3811635}}}
	fields, _ := clusterFields(c, members)
	if got, _ := fieldValue(fields, "Memory"); got != "3.6T used of 6.3T (58%)" {
		t.Fatalf("Memory = %q, want 3.6T used of 6.3T (58%%)", got)
	}
}

// TestVMDetailSizesMatch checks that a VM's committed storage reads like its
// memory, 40G rather than 40.0G, in the browse detail (issue #475).
func TestVMDetailSizesMatch(t *testing.T) {
	b := twoHealthy()
	inv := b.inventories["prod"]
	inv.VMs[0].MemoryMB, inv.VMs[0].StorageGB = 8192, 40
	m := newTestModel(t, b, Options{Current: "prod"})
	m.width, m.height = 100, 40
	press(t, m, "enter")
	if _, line := lineWith(m.View(), "Committed storage"); !strings.Contains(line, "40G") || strings.Contains(line, "40.0G") {
		t.Fatalf("Committed storage = %q, want 40G", line)
	}
}

// TestMemoryChartsUseSizeUnits checks that the balloon chart writes memory
// the way every other size is written (issue #475).
func TestMemoryChartsUseSizeUnits(t *testing.T) {
	for _, tc := range []struct {
		mib  float64
		want string
	}{{0, "0M"}, {512, "512M"}, {16384, "16.0G"}} {
		if got := fmtMem(tc.mib); got != tc.want {
			t.Errorf("fmtMem(%v) = %q, want %q", tc.mib, got, tc.want)
		}
	}
}

// TestContextsFooterOffersQuit checks that the Contexts screen names q quit
// like every other screen (issue #475).
func TestContextsFooterOffersQuit(t *testing.T) {
	for _, demo := range []bool{false, true} {
		m := newTestModel(t, twoHealthy(), Options{Current: "prod", Demo: demo})
		m.width, m.height = 140, 30
		press(t, m, "c")
		if m.mode != modeContexts {
			t.Fatalf("c opened mode %v", m.mode)
		}
		if footer := ansi.Strip(footerText(m)); !strings.Contains(footer, "q quit") {
			t.Fatalf("demo=%v Contexts footer lacks q quit: %q", demo, footer)
		}
	}
}
