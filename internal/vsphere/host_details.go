package vsphere

import (
	"crypto/x509"
	"encoding/pem"
	"strings"

	"github.com/vmware/govmomi/vim25/mo"
)

// hostDetailProps are read with the host configuration, beside
// summary.hardware, summary.config.product and config.network, which
// hostProps and hostConfigProps already fetch.
var hostDetailProps = []string{
	"summary.currentEVCModeKey",
	"summary.maxEVCModeKey",
	"runtime.bootTime",
	"capability.vmotionSupported",
	"capability.storageVMotionSupported",
	"hardware.systemInfo",
	"hardware.biosInfo",
	"config.hyperThread",
	"config.dateTimeInfo",
	"config.service",
	"config.certificate",
}

// mapHostDetails reads vHost's hardware, time, name service, vMotion and
// certificate columns from a host fetched with hostDetailProps.
func mapHostDetails(m *mo.HostSystem) *HostDetails {
	d := &HostDetails{
		CurrentEVC: m.Summary.CurrentEVCModeKey,
		MaxEVC:     m.Summary.MaxEVCModeKey,
		BootTime:   m.Runtime.BootTime,
	}
	if p := m.Summary.Config.Product; p != nil {
		d.ProductName = strings.TrimSpace(p.FullName)
	}
	if hw := m.Summary.Hardware; hw != nil {
		d.CPUModel = strings.TrimSpace(hw.CpuModel)
		d.CPUPackages = int32(hw.NumCpuPkgs)
		d.UUID = hw.Uuid
	}
	if hw := m.Hardware; hw != nil {
		d.SerialNumber = strings.TrimSpace(hw.SystemInfo.SerialNumber)
		if d.UUID == "" {
			d.UUID = hw.SystemInfo.Uuid
		}
		if bios := hw.BiosInfo; bios != nil {
			d.BIOSVendor = strings.TrimSpace(bios.Vendor)
			d.BIOSVersion = strings.TrimSpace(bios.BiosVersion)
			d.BIOSDate = bios.ReleaseDate
		}
	}
	if c := m.Capability; c != nil {
		d.VMotion, d.StorageVMotion = boolPtr(c.VmotionSupported), boolPtr(c.StorageVMotionSupported)
	}
	cfg := m.Config
	if cfg == nil {
		return d
	}
	if ht := cfg.HyperThread; ht != nil {
		d.HTAvailable, d.HTActive = boolPtr(ht.Available), boolPtr(ht.Active)
	}
	if dt := cfg.DateTimeInfo; dt != nil {
		d.TimeZone, d.TimeZoneName = dt.TimeZone.Key, dt.TimeZone.Name
		d.GMTOffset = int32Ptr(dt.TimeZone.GmtOffset)
		if ntp := dt.NtpConfig; ntp != nil {
			d.NTPServers = ntp.Server
		}
	}
	if svc := cfg.Service; svc != nil {
		for _, s := range svc.Service {
			if s.Key == "ntpd" {
				d.NTPDRunning = boolPtr(s.Running)
			}
		}
	}
	if network := cfg.Network; network != nil && network.DnsConfig != nil {
		dns := network.DnsConfig.GetHostDnsConfig()
		d.DNSDHCP, d.Domain, d.DNSServers = boolPtr(dns.Dhcp), dns.DomainName, dns.Address
	}
	d.Certificate = parseHostCertificate(cfg.Certificate)
	return d
}

// parseHostCertificate reads the first certificate of a PEM bundle, nil
// when there is none or it does not parse.
func parseHostCertificate(data []byte) *HostCertificate {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}
	return &HostCertificate{Issuer: cert.Issuer.String(), Subject: cert.Subject.String(), NotBefore: cert.NotBefore.UTC(), NotAfter: cert.NotAfter.UTC()}
}
