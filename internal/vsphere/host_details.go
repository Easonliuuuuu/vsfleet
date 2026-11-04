package vsphere

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"strings"

	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
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
	"configStatus",
	"runtime.inQuarantineMode",
	"hardware.cpuPowerManagementInfo",
	"config.powerSystemInfo.currentPolicy",
	"config.domainList",
	"configManager.advancedOption",
}

// versionedHostDetailProps are read only from a vCenter whose API is at
// least since: an older one rejects the whole retrieval for an unknown
// property.
var versionedHostDetailProps = []struct{ since, path string }{
	{"7.0.3.0", "hardware.memoryTieringType"},
}

// hostDetailPropsForAPI is hostDetailProps with the versioned properties
// the vCenter's API supports.
func hostDetailPropsForAPI(apiVersion string) []string {
	out := append([]string{}, hostDetailProps...)
	for _, p := range versionedHostDetailProps {
		if apiAtLeast(apiVersion, p.since) {
			out = append(out, p.path)
		}
	}
	return out
}

// mapHostDetails reads vHost's hardware, time, name service, vMotion and
// certificate columns from a host fetched with hostDetailProps.
func mapHostDetails(m *mo.HostSystem) *HostDetails {
	d := &HostDetails{
		CurrentEVC:   m.Summary.CurrentEVCModeKey,
		MaxEVC:       m.Summary.MaxEVCModeKey,
		BootTime:     m.Runtime.BootTime,
		ConfigStatus: string(m.ConfigStatus),
		InQuarantine: m.Runtime.InQuarantineMode,
	}
	if p := m.Summary.Config.Product; p != nil {
		d.ProductName = strings.TrimSpace(p.FullName)
	}
	if hw := m.Summary.Hardware; hw != nil {
		d.CPUModel = strings.TrimSpace(hw.CpuModel)
		d.CPUPackages = int32(hw.NumCpuPkgs)
		d.UUID = hw.Uuid
		d.ServiceTag, d.OEMString = systemIdentifiers(hw.OtherIdentifyingInfo)
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
		d.MemoryTiering = hw.MemoryTieringType
		if pm := hw.CpuPowerManagementInfo; pm != nil {
			d.CPUPowerPolicy, d.CPUPowerSupport = strings.TrimSpace(pm.CurrentPolicy), strings.TrimSpace(pm.HardwareSupport)
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
	if network := cfg.Network; network != nil {
		if network.DnsConfig != nil {
			dns := network.DnsConfig.GetHostDnsConfig()
			d.DNSDHCP, d.Domain, d.DNSServers, d.SearchDomains = boolPtr(dns.Dhcp), dns.DomainName, dns.Address, dns.SearchDomain
		}
		if network.IpRouteConfig != nil {
			route := network.IpRouteConfig.GetHostIpRouteConfig()
			d.Gateway, d.IPv6Gateway = route.DefaultGateway, route.IpV6DefaultGateway
		}
	}
	if power := cfg.PowerSystemInfo; power != nil {
		d.HostPowerPolicy = power.CurrentPolicy.ShortName
	}
	d.DomainList = cfg.DomainList
	d.Certificate = parseHostCertificate(cfg.Certificate)
	return d
}

// systemIdentifiers reads the service tag and the OEM specific strings
// from a host's other identifying information. A host can report several
// OEM strings; they are joined with ", ".
func systemIdentifiers(info []types.HostSystemIdentificationInfo) (serviceTag, oem string) {
	var oems []string
	for _, id := range info {
		value := strings.TrimSpace(id.IdentifierValue)
		if value == "" || id.IdentifierType == nil {
			continue
		}
		switch id.IdentifierType.GetElementDescription().Key {
		case "ServiceTag":
			serviceTag = value
		case "OemSpecificString":
			oems = append(oems, value)
		}
	}
	return serviceTag, strings.Join(oems, ", ")
}

// atsSettings are the two VMFS advanced settings vHost reports. Their key
// selectors read one setting each rather than every advanced option.
var atsSettings = []string{`setting["VMFS3.UseATSForHBOnVMFS5"]`, `setting["VMFS3.HardwareAcceleratedLocking"]`}

// hostATSSettings reads ATS Heartbeat and ATS Locking from each host's
// advanced option manager in one retrieval, keyed by the manager. A setting
// a host does not have, or a manager that cannot be read, is left out.
func hostATSSettings(ctx context.Context, c *Client, managers []types.ManagedObjectReference) map[types.ManagedObjectReference][2]*int64 {
	out := make(map[types.ManagedObjectReference][2]*int64, len(managers))
	if len(managers) == 0 {
		return out
	}
	var content []types.ObjectContent
	if err := property.DefaultCollector(c.VIM()).Retrieve(ctx, managers, atsSettings, &content); err != nil {
		return out
	}
	return atsFromContent(content)
}

// atsFromContent decodes the settings hostATSSettings retrieved. A setting
// the host lacks comes back in the object's missing set and stays nil.
func atsFromContent(content []types.ObjectContent) map[types.ManagedObjectReference][2]*int64 {
	out := make(map[types.ManagedObjectReference][2]*int64, len(content))
	for _, obj := range content {
		var values [2]*int64
		for _, p := range obj.PropSet {
			for i, path := range atsSettings {
				if p.Name == path {
					values[i] = optionValueInt(p.Val)
				}
			}
		}
		out[obj.Obj] = values
	}
	return out
}

// optionValueInt reads an integer setting. The property collector returns
// an OptionValue by value, a decoded BaseOptionValue as a pointer.
func optionValueInt(v any) *int64 {
	var value any
	switch opt := v.(type) {
	case types.OptionValue:
		value = opt.Value
	case types.BaseOptionValue:
		value = opt.GetOptionValue().Value
	default:
		return nil
	}
	switch n := value.(type) {
	case int32:
		return int64Ptr(int64(n))
	case int64:
		return int64Ptr(n)
	case int:
		return int64Ptr(int64(n))
	}
	return nil
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
