package tui

import (
	"fmt"

	"github.com/easonliuuuuu/vsfleet/internal/config"
)

// vsphereClientSection is the inventory category the modern vSphere Client
// groups an object under, which is also the fixed segment of its own URL
// path ("/ui/app/vm", "/ui/app/host", ...). vApp objects are approximated as
// "vm" — vCenter's own client nests them under the VMs and Templates view —
// pending a check against a real client of the exact route it expects; see
// the plan's noted risk that this format has not been verified against a
// live 7.x/8.x vCenter.
func vsphereClientSection(morefKind string) (string, bool) {
	switch morefKind {
	case "VirtualMachine", "VirtualApp":
		return "vm", true
	case "HostSystem":
		return "host", true
	case "ClusterComputeResource", "ComputeResource":
		return "cluster", true
	case "Datastore":
		return "datastore", true
	case "Network", "DistributedVirtualPortgroup", "OpaqueNetwork":
		return "network", true
	default:
		return "", false
	}
}

// vsphereClientURL builds a deep link into the vSphere Client's inventory
// view for one managed object. The link needs the vCenter's own server GUID
// alongside the moref: a moref value is only unique within one vCenter's
// database, so the same "vm-1234" could name a different machine on another
// vCenter in the same estate, and the GUID is what tells the client which
// database to resolve it against.
func vsphereClientURL(cc *config.Context, morefKind, moref, instanceID string) (string, bool) {
	if instanceID == "" || moref == "" {
		return "", false
	}
	section, ok := vsphereClientSection(morefKind)
	if !ok {
		return "", false
	}
	u, err := cc.URL()
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("%s://%s/ui/app/%s;nav=v/urn:vmomi:%s:%s:%s",
		u.Scheme, u.Host, section, morefKind, moref, instanceID), true
}

// hostClientURL builds a link to the ESXi host's own standalone Embedded
// Host Client. Unlike vsphereClientURL this needs no vCenter identity at
// all — the host serves its own client directly, so its address is enough.
func hostClientURL(address string) (string, bool) {
	if address == "" {
		return "", false
	}
	return "https://" + address + "/ui", true
}
