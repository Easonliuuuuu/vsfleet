package rvimport

import (
	"strconv"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// applyHostConfigCells reads the vHost status, power, storage locking, name
// service and identifier columns inventory schema 30 added.
func applyHostConfigCells(t sheetTable, row []string, d *vsphere.HostDetails) {
	d.ConfigStatus = t.cell(row, "Config status")
	d.InQuarantine = parseOptBool(t.cell(row, "in Quarantine Mode"))
	d.MemoryTiering = t.cell(row, "Memory Tiering Type")
	d.ATSHeartbeat = parseOptInt64(t.cell(row, "ATS Heartbeat"))
	d.ATSLocking = parseOptInt64(t.cell(row, "ATS Locking"))
	d.CPUPowerPolicy = t.cell(row, "Current CPU power man. policy")
	d.CPUPowerSupport = t.cell(row, "Supported CPU power man.")
	d.HostPowerPolicy = t.cell(row, "Host Power Policy")
	d.DomainList = splitList(t.cell(row, "Domain List"))
	d.SearchDomains = splitList(t.cell(row, "DNS Search Order"))
	d.ServiceTag = t.cell(row, "Service tag")
	// OEM strings can hold commas of their own, so the cell is kept whole.
	d.OEMString = t.cell(row, "OEM specific string")
}

// applyVMKGateways reads vSC_VMK's Gateway and IP 6 Gateway, which RVTools
// repeats on every adapter of a host, into the host's details.
func applyVMKGateways(t sheetTable, row []string, h *vsphere.Host) {
	gateway, gateway6 := t.cell(row, "Gateway"), t.cell(row, "IP 6 Gateway")
	if gateway == "" && gateway6 == "" {
		return
	}
	if h.Details == nil {
		h.Details = &vsphere.HostDetails{}
	}
	if h.Details.Gateway == "" {
		h.Details.Gateway = gateway
	}
	if h.Details.IPv6Gateway == "" {
		h.Details.IPv6Gateway = gateway6
	}
}

// parseHexWWN reads vHBA's WWN, the port world wide name RVTools writes in
// hexadecimal. The 64 bits are kept as vSphere's signed long holds them.
func parseHexWWN(value string) *int64 {
	value = strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(value), ":", ""), "0x")
	if value == "" {
		return nil
	}
	if len(value) > 16 || strings.Trim(value, "0123456789abcdefABCDEF") != "" {
		return nil
	}
	// Each half fits in an int64, so a WWN whose top bit is set wraps to the
	// negative long vSphere reports without converting from an unsigned type.
	value = strings.Repeat("0", 16-len(value)) + value
	hi, err := strconv.ParseInt(value[:8], 16, 64)
	if err != nil {
		return nil
	}
	lo, err := strconv.ParseInt(value[8:], 16, 64)
	if err != nil {
		return nil
	}
	v := hi<<32 | lo
	return &v
}
