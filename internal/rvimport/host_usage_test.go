package rvimport

import (
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The usage cells are those of the real RVTools 4.8.1.4 export of the nested
// lab for 192.168.150.11: 4 cores at 2,496 MHz and 20,412 MiB.
func TestVHostUsagePercentagesAreReadAsUsage(t *testing.T) {
	path := writeFixtureWorkbook(t, nil)
	for _, tc := range []struct {
		name          string
		cpu, memory   string
		wantCPU, want int64
	}{
		{"lab", "40", "80", 3994, 16330},
		{"not reported", "", "", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openFixture(t, path)
			for header, value := range map[string]string{"Speed": "2,496", "# Cores": "4", "# Memory": "20,412", "CPU usage %": tc.cpu, "Memory usage %": tc.memory} {
				setColumnValue(t, f, sheetVHost, header, 2, value)
			}
			result, err := Parse(f, Options{})
			if err != nil {
				t.Fatal(err)
			}
			h := findHost(result, "alpha", "esx-a1")
			if h == nil {
				t.Fatal("esx-a1 not imported")
			}
			if h.CPUUsageMHz != tc.wantCPU || h.MemoryUsageMB != tc.want {
				t.Errorf("usage = %d MHz, %d MiB; want %d MHz, %d MiB", h.CPUUsageMHz, h.MemoryUsageMB, tc.wantCPU, tc.want)
			}
		})
	}
}

func findHost(result *Result, contextName, name string) *vsphere.Host {
	for _, c := range result.contexts {
		if c.name != contextName {
			continue
		}
		for _, h := range c.hosts {
			if h.Name == name {
				return h
			}
		}
	}
	return nil
}
