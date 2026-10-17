package report

import (
	"math"
	"strings"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// hostHeaders are RVTools 4.8.1.4's vHost columns that vsfleet captures, in
// RVTools' order, followed by vsfleet's context column.
var hostHeaders = []string{"Host", "Datacenter", "Cluster", "in Maintenance Mode", "CPU Model", "Speed", "HT Available", "HT Active",
	"# CPU", "Cores per CPU", "# Cores", "CPU usage %", "# Memory", "Memory usage %", "# NICs", "# HBAs", "# VMs total", "# VMs", "VMs per Core",
	"# vCPUs", "vCPUs per Core", "vRAM", "VMotion support", "Storage VMotion support", "Current EVC", "Max EVC", "ESX Version", "Boot time",
	"DNS Servers", "DHCP", "Domain", "NTP Server(s)", "NTPD running", "Time Zone", "Time Zone Name", "GMT Offset", "Vendor", "Model",
	"Serial number", "BIOS Vendor", "BIOS Version", "BIOS Date", "Certificate Issuer", "Certificate Start Date", "Certificate Expiry Date",
	"Certificate Subject", "Object ID", "UUID", "VI SDK Server", "VI SDK UUID", "vsfleet Context"}

// hostRow renders one vHost row. A host captured before its details were
// read has a nil Details, and every detail cell stays empty.
func hostRow(data assessment.ExportData, densities map[hostKey]hostDensity, r assessment.ResourceObservation, h vsphere.Host) []any {
	d := h.Details
	if d == nil {
		d = &vsphere.HostDetails{}
	}
	var packages, coresPerCPU any
	if d.CPUPackages > 0 {
		packages = d.CPUPackages
		if h.CPUCores > 0 {
			coresPerCPU = h.CPUCores / d.CPUPackages
		}
	}
	version := h.Version
	if d.ProductName != "" {
		version = d.ProductName
	}
	var certIssuer, certStart, certExpiry, certSubject any
	if c := d.Certificate; c != nil {
		certIssuer, certStart, certExpiry, certSubject = optionalString(c.Issuer), c.NotBefore.UTC(), c.NotAfter.UTC(), optionalString(c.Subject)
	}
	var gmtOffset any
	if d.GMTOffset != nil {
		gmtOffset = *d.GMTOffset
	}
	row := []any{nonempty(h.Name, r.Name), h.Datacenter, h.Cluster, h.InMaintenance, optionalString(d.CPUModel), h.CPUMHz,
		optionalBool(d.HTAvailable), optionalBool(d.HTActive), packages, coresPerCPU, h.CPUCores, hostCPUPercent(h), h.MemoryMB,
		hostMemoryPercent(h)}
	row = append(append(row, hostAdapterCounts(data, h)...), h.VMCount)
	row = append(row, hostDensityCells(data, densities, r.Context, h)...)
	return append(row, optionalBool(d.VMotion), optionalBool(d.StorageVMotion), optionalString(d.CurrentEVC), optionalString(d.MaxEVC),
		version, optionalTime(d.BootTime), optionalString(strings.Join(d.DNSServers, ", ")), optionalBool(d.DNSDHCP), optionalString(d.Domain),
		optionalString(strings.Join(d.NTPServers, ", ")), optionalBool(d.NTPDRunning), optionalString(d.TimeZone), optionalString(d.TimeZoneName),
		gmtOffset, h.Vendor, h.Model, optionalString(d.SerialNumber), optionalString(d.BIOSVendor), optionalString(d.BIOSVersion),
		optionalTime(d.BIOSDate), certIssuer, certStart, certExpiry, certSubject, nonempty(h.ID, r.ID), optionalString(d.UUID),
		sdkServer(data, r.Context), r.VCenterID, r.Context)
}

// hostAdapterCounts writes # NICs and # HBAs, both empty unless the host's
// configuration was read. HostSystem.Config is nil for a disconnected host,
// so an empty list there is no evidence of a host without adapters. A host
// vCenter manages always has a management VMkernel adapter, so any host
// networking shows the configuration was read, and the storage adapters with
// it. An RVTools import claims a schema below 9: its vNIC and vHBA rows come
// from separate worksheets, and one missing would read as no adapters.
func hostAdapterCounts(data assessment.ExportData, h vsphere.Host) []any {
	networking := len(h.NICs) + len(h.VSwitches) + len(h.PortGroups) + len(h.VMKs) + len(h.ProxySwitches)
	if !inventoryAtLeast(data.Run.InventorySchemaVersion, 9) || networking == 0 {
		return []any{nil, nil}
	}
	return []any{len(h.NICs), len(h.HBAs)}
}

func optionalTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC()
}

// hostDensity is the powered-on VM load on one host, as RVTools counts it:
// templates and powered-off VMs are left out.
type hostDensity struct {
	vms    int
	vCPUs  int64
	vRAMMB int64
}

type hostKey struct{ context, host string }

// hostDensities sums the powered-on VMs on each host of every context whose
// VM collection completed.
func hostDensities(data assessment.ExportData) map[hostKey]hostDensity {
	out := make(map[hostKey]hostDensity)
	for _, item := range data.VMs {
		obs, vm := item.Observation, item.Observation.VM
		if vm.IsTemplate || vm.PowerState != "poweredOn" || vm.Host == "" {
			continue
		}
		k := hostKey{obs.Context, vm.Host}
		d := out[k]
		d.vms++
		d.vCPUs += int64(vm.CPU)
		d.vRAMMB += vm.MemoryMB
		out[k] = d
	}
	return out
}

// hostDensityCells writes # VMs, VMs per Core, # vCPUs, vCPUs per Core and
// vRAM. They stay empty for a context whose VM collection did not complete,
// rather than reporting a host with no VMs.
func hostDensityCells(data assessment.ExportData, densities map[hostKey]hostDensity, context string, h vsphere.Host) []any {
	if !vmCollectionComplete(data, context) {
		return make([]any, 5)
	}
	d := densities[hostKey{context, h.Name}]
	var vmsPerCore, vCPUsPerCore any
	if h.CPUCores > 0 {
		vmsPerCore = perCore(int64(d.vms), h.CPUCores)
		vCPUsPerCore = perCore(d.vCPUs, h.CPUCores)
	}
	return []any{d.vms, vmsPerCore, d.vCPUs, vCPUsPerCore, d.vRAMMB}
}

func vmCollectionComplete(data assessment.ExportData, context string) bool {
	for _, c := range data.Contexts {
		if c.Name == context {
			return c.VMStatus == "success" || c.VMStatus == "empty"
		}
	}
	return false
}

// perCore is a count per physical core, rounded to two decimals as RVTools
// writes it.
func perCore(n int64, cores int32) float64 {
	return math.Round(float64(n)/float64(cores)*100) / 100
}
