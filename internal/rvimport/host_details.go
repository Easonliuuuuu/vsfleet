package rvimport

import (
	"reflect"
	"regexp"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// esxProductName matches RVTools' ESX Version, for example
// "VMware ESXi 8.0.3 build-24784735".
var esxProductName = regexp.MustCompile(`^VMware ESXi (\S+) build-(\d+)$`)

// applyHostDetails reads vHost's hardware, time, name service, vMotion and
// certificate columns. Details stays nil when the row carries none of them,
// as for an older vsfleet workbook, so the host reads as "not read" rather
// than as hardware with nothing reported. Every cell is read first so the
// column report sees each one.
func applyHostDetails(t sheetTable, row []string, h *vsphere.Host, timezone *time.Location) {
	d := vsphere.HostDetails{
		CPUModel:       t.cell(row, "CPU Model"),
		CPUPackages:    int32(parseInt(t.cell(row, "# CPU"))),
		HTAvailable:    parseOptBool(t.cell(row, "HT Available")),
		HTActive:       parseOptBool(t.cell(row, "HT Active")),
		VMotion:        parseOptBool(t.cell(row, "VMotion support")),
		StorageVMotion: parseOptBool(t.cell(row, "Storage VMotion support")),
		CurrentEVC:     t.cell(row, "Current EVC"),
		MaxEVC:         t.cell(row, "Max EVC"),
		BootTime:       optionalTime(t.cell(row, "Boot time"), timezone),
		DNSServers:     splitList(t.cell(row, "DNS Servers")),
		DNSDHCP:        parseOptBool(t.cell(row, "DHCP")),
		Domain:         t.cell(row, "Domain"),
		NTPServers:     splitList(t.cell(row, "NTP Server(s)")),
		NTPDRunning:    parseOptBool(t.cell(row, "NTPD running")),
		TimeZone:       t.cell(row, "Time Zone"),
		TimeZoneName:   t.cell(row, "Time Zone Name"),
		GMTOffset:      parseOptInt32(t.cell(row, "GMT Offset")),
		SerialNumber:   t.cell(row, "Serial number"),
		BIOSVendor:     t.cell(row, "BIOS Vendor"),
		BIOSVersion:    t.cell(row, "BIOS Version"),
		BIOSDate:       optionalTime(t.cell(row, "BIOS Date"), timezone),
		UUID:           t.cell(row, "UUID"),
	}
	issuer, subject := t.cell(row, "Certificate Issuer"), t.cell(row, "Certificate Subject")
	notBefore, notAfter := optionalTime(t.cell(row, "Certificate Start Date"), timezone), optionalTime(t.cell(row, "Certificate Expiry Date"), timezone)
	if issuer != "" || subject != "" || notBefore != nil || notAfter != nil {
		c := vsphere.HostCertificate{Issuer: issuer, Subject: subject}
		if notBefore != nil {
			c.NotBefore = *notBefore
		}
		if notAfter != nil {
			c.NotAfter = *notAfter
		}
		d.Certificate = &c
	}
	version := t.cell(row, "ESX Version")
	if m := esxProductName.FindStringSubmatch(version); m != nil {
		h.Version, h.Build, d.ProductName = m[1], m[2], version
	} else {
		h.Version = version
	}
	if reflect.DeepEqual(d, vsphere.HostDetails{}) {
		return
	}
	h.Details = &d
}

func optionalTime(s string, timezone *time.Location) *time.Time {
	v, ok := parseSnapshotTime(s, timezone)
	if !ok {
		return nil
	}
	return &v
}
