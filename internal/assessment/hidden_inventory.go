package assessment

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// CollectionPartial marks a collection that returned no error but is known to
// be missing objects the rest of the capture still references.
const CollectionPartial = "partial"

// morefKey matches the managed-object keys vCenter hands out for networks. An
// opaque (NSX) network id is not one, and is not enumerated with them, so it
// never counts as a dangling reference.
var morefKey = regexp.MustCompile(`^(dvportgroup|network)-\d+$`)

// maxNetworkChecks bounds how many unresolved network references one capture
// reads back from vCenter. Past it, a reference stays unconfirmed and is
// treated as possibly hidden, as it was before the check existed.
const maxNetworkChecks = 64

// networkIndex is what a successful network collection returned, by moref
// key and by name.
type networkIndex struct {
	keys, names map[string]bool
}

func newNetworkIndex(r *ContextResult) networkIndex {
	idx := networkIndex{keys: map[string]bool{}, names: map[string]bool{}}
	for _, n := range decodeResources[vsphere.Network](r, "network") {
		idx.keys[strings.ToLower(n.ID)] = true
		idx.names[strings.ToLower(n.Name)] = true
	}
	return idx
}

// unresolved reports whether nic names a vCenter network the collection did
// not return.
func (idx networkIndex) unresolved(nic vsphere.VMNIC) bool {
	return morefKey.MatchString(nic.NetworkID) && !idx.keys[strings.ToLower(nic.NetworkID)] && !idx.names[strings.ToLower(nic.Network)]
}

// unresolvedNetworks returns the distinct network ids VM NICs reference that a
// successful network collection did not return. A collection that did not
// succeed has nothing to contradict, so it yields none.
func unresolvedNetworks(r *ContextResult) []string {
	succeeded := false
	for _, c := range r.Collections {
		if c.Kind == "network" && Successful(c.Status) {
			succeeded = true
		}
	}
	if !succeeded {
		return nil
	}
	idx := newNetworkIndex(r)
	seen := map[string]bool{}
	var ids []string
	for _, o := range r.VMs {
		for _, nic := range o.VM.NICs {
			key := strings.ToLower(nic.NetworkID)
			if idx.unresolved(nic) && !seen[key] {
				seen[key] = true
				ids = append(ids, nic.NetworkID)
			}
		}
	}
	return ids
}

// confirmDeletedNetworks reads each unresolved network back from vCenter and
// returns the ids, lowercased, that it confirmed no longer exist. A read that
// fails or is not attempted leaves its id unconfirmed: the safe answer, since
// an unconfirmed reference keeps the network collection partial.
func confirmDeletedNetworks(ctx context.Context, client *vsphere.Client, ids []string) map[string]bool {
	deleted := map[string]bool{}
	for i, id := range ids {
		if i == maxNetworkChecks || ctx.Err() != nil {
			break
		}
		if gone, err := client.NetworkDeleted(ctx, id); err == nil && gone {
			deleted[strings.ToLower(id)] = true
		}
	}
	return deleted
}

// markHiddenInventory demotes host, network and dvswitch collections that
// look complete but are contradicted by the rest of the same capture.
//
// vCenter answers a NoAccess override with silence, not a fault: the property
// collector simply leaves the object out. A collection that lists nothing, or
// lists fewer objects than are referenced, would otherwise read as a confirmed
// answer, and every rule that gates on it would report a clean result.
//
// A deleted object drops out of the listing the same way. deleted holds the
// lowercased network ids vCenter confirmed are gone; a NIC that references one
// is a dangling backing, marked NetworkMissing, and is no evidence of hidden
// inventory.
func markHiddenInventory(r *ContextResult, deleted map[string]bool) {
	hosts := decodeResources[vsphere.Host](r, "host")
	clusters := decodeResources[vsphere.Cluster](r, "cluster")
	switches := decodeResources[vsphere.DVSwitch](r, "dvswitch")
	networks := newNetworkIndex(r)

	hostNames, hostsByCluster := map[string]bool{}, map[string]int{}
	for _, h := range hosts {
		hostNames[strings.ToLower(h.Name)] = true
		hostsByCluster[strings.ToLower(h.Cluster)]++
	}
	switchUUIDs := map[string]bool{}
	for _, s := range switches {
		switchUUIDs[strings.ToLower(s.UUID)] = true
	}

	var hiddenHost, hiddenNetwork, hiddenSwitch string
	for _, c := range clusters {
		if !c.Standalone && c.Hosts > hostsByCluster[strings.ToLower(c.Name)] {
			hiddenHost = fmt.Sprintf("cluster %q has %d host(s) but only %d were returned", c.Name, c.Hosts, hostsByCluster[strings.ToLower(c.Name)])
			break
		}
	}
	for i := range r.VMs {
		vm := &r.VMs[i].VM
		if hiddenHost == "" {
			switch {
			case vm.Host != "" && !hostNames[strings.ToLower(vm.Host)]:
				hiddenHost = fmt.Sprintf("VM %q runs on host %q, which was not returned", vm.Name, vm.Host)
			case vm.Host == "" && vm.PowerState == "poweredOn":
				hiddenHost = fmt.Sprintf("powered-on VM %q reports no host", vm.Name)
			}
		}
		for j := range vm.NICs {
			nic := &vm.NICs[j]
			if networks.unresolved(*nic) {
				switch {
				case deleted[strings.ToLower(nic.NetworkID)]:
					nic.NetworkMissing = true
				case hiddenNetwork == "":
					hiddenNetwork = fmt.Sprintf("VM %q uses network %q (%s), which was not returned", vm.Name, nic.Network, nic.NetworkID)
				}
			}
			if hiddenSwitch == "" && nic.SwitchID != "" && !switchUUIDs[strings.ToLower(nic.SwitchID)] {
				hiddenSwitch = fmt.Sprintf("VM %q uses distributed switch %s, which was not returned", vm.Name, nic.SwitchID)
			}
		}
	}

	demote := func(kind, reason string) {
		if reason == "" {
			return
		}
		for i := range r.Collections {
			c := &r.Collections[i]
			if c.Kind == kind && Successful(c.Status) {
				c.Status = CollectionPartial
				c.Error = "possible permission-hidden inventory: " + reason
			}
		}
	}
	demote("host", hiddenHost)
	demote("network", hiddenNetwork)
	demote("dvswitch", hiddenSwitch)
}

func decodeResources[T any](r *ContextResult, kind string) []T {
	var out []T
	for _, c := range r.Collections {
		if c.Kind != kind || !Successful(c.Status) {
			continue
		}
		for _, res := range c.Resources {
			var v T
			if json.Unmarshal(res.Payload, &v) == nil {
				out = append(out, v)
			}
		}
	}
	return out
}
