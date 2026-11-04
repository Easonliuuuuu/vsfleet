package report

import (
	"fmt"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// hbaWWN is vHBA's WWN, which RVTools documents as the adapter's port world
// wide name: a Fibre Channel adapter's WWPN in hexadecimal, without the
// precision a spreadsheet number loses. Other adapters have none.
func hbaWWN(hba vsphere.HostHBA) any {
	if hba.WWPN == nil {
		return nil
	}
	return fmt.Sprintf("%X", uint64(*hba.WWPN))
}

// vmkIPv6 is vSC_VMK's IP 6 Address: the adapter's IPv6 addresses without
// their prefix length, joined with ", ".
func vmkIPv6(vmk vsphere.HostVMKernel) any {
	addresses := make([]string, 0, len(vmk.IPv6))
	for _, a := range vmk.IPv6 {
		address, _, _ := strings.Cut(a, "/")
		addresses = append(addresses, address)
	}
	return optionalString(strings.Join(addresses, ", "))
}

// vmkGateways are vSC_VMK's Gateway and IP 6 Gateway, RVTools' "default
// gateway address": the host's default TCP/IP stack gateways, shown on every
// adapter, also one on another stack.
func vmkGateways(h vsphere.Host) (any, any) {
	if h.Details == nil {
		return nil, nil
	}
	return optionalString(h.Details.Gateway), optionalString(h.Details.IPv6Gateway)
}
