// Package apiresource is the one vocabulary of object kinds the command line
// accepts. Every command that takes a kind, by flag or argument, resolves it
// here, so a short name that works in one place works in all of them.
package apiresource

import (
	"fmt"
	"slices"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// Kind describes one class of object the way kubectl's api-resources does.
type Kind struct {
	// Name is the singular, canonical name, e.g. "vm".
	Name string `json:"name"`
	// Plural is the name a listing is asked for by, e.g. "vms".
	Plural string `json:"plural"`
	// ShortNames are advertised abbreviations and synonyms, e.g. "ds".
	ShortNames []string `json:"short_names,omitempty"`
	// Aliases are accepted but not advertised: long spellings and the
	// plurals of short names.
	Aliases []string `json:"-"`
	// Inventory is the vSphere inventory kind this maps to, or empty for a
	// kind that is not one (a host subresource, a snapshot, an event).
	Inventory vsphere.Kind `json:"inventory_kind,omitempty"`
	// Live is true when vsfleet reads the kind from a connected vCenter.
	Live bool `json:"live"`
	// Stored is true when an assessment capture records the kind.
	Stored bool `json:"stored"`
	// History is true for a kind the local history database keeps as an
	// object of its own, such as a capture, rather than inside a capture.
	History bool `json:"history,omitempty"`
	// Description is a one-line summary for api-resources.
	Description string `json:"description"`
}

// Names returns every spelling that resolves to k: the name, the plural,
// the short names and the aliases.
func (k Kind) Names() []string {
	out := []string{k.Name, k.Plural}
	out = append(out, k.ShortNames...)
	return append(out, k.Aliases...)
}

var registry = []Kind{
	{Name: "vm", Plural: "vms", Aliases: []string{"virtualmachine", "virtualmachines"}, Inventory: vsphere.KindVM, Live: true, Stored: true, Description: "Virtual machines"},
	{Name: "template", Plural: "templates", ShortNames: []string{"tmpl", "tpl"}, Inventory: vsphere.KindTemplate, Live: true, Stored: true, Description: "VM templates"},
	{Name: "host", Plural: "hosts", ShortNames: []string{"esxi", "node"}, Aliases: []string{"nodes"}, Inventory: vsphere.KindHost, Live: true, Stored: true, Description: "ESXi hosts"},
	{Name: "cluster", Plural: "clusters", ShortNames: []string{"cl"}, Inventory: vsphere.KindCluster, Live: true, Stored: true, Description: "Compute clusters"},
	{Name: "vapp", Plural: "vapps", Aliases: []string{"virtualapp", "virtualapps"}, Inventory: vsphere.KindVApp, Live: true, Stored: true, Description: "vSphere vApps"},
	{Name: "datastore", Plural: "datastores", ShortNames: []string{"ds"}, Inventory: vsphere.KindDatastore, Live: true, Stored: true, Description: "Datastores"},
	{Name: "network", Plural: "networks", ShortNames: []string{"net"}, Inventory: vsphere.KindNetwork, Live: true, Stored: true, Description: "Networks and port groups, as vCenter inventories them"},
	{Name: "dvswitch", Plural: "dvswitches", ShortNames: []string{"dvs", "vds"}, Aliases: []string{"distributed-switch", "distributed-switches"}, Inventory: vsphere.KindDVSwitch, Live: true, Stored: true, Description: "Distributed virtual switches"},
	{Name: "dvportgroup", Plural: "dvportgroups", ShortNames: []string{"dvpg"}, Live: true, Stored: true, Description: "Distributed port groups"},
	{Name: "resourcepool", Plural: "resourcepools", ShortNames: []string{"rp"}, Aliases: []string{"resource-pool", "resource-pools", "pool", "pools"}, Inventory: vsphere.KindResourcePool, Live: true, Stored: true, Description: "Resource pools"},
	{Name: "snapshot", Plural: "snapshots", ShortNames: []string{"snap"}, Aliases: []string{"snaps"}, Live: true, Stored: true, Description: "VM snapshots"},
	{Name: "hba", Plural: "hbas", Live: true, Stored: true, Description: "Host bus adapters"},
	{Name: "pnic", Plural: "pnics", Live: true, Stored: true, Description: "Host physical NICs"},
	{Name: "vswitch", Plural: "vswitches", ShortNames: []string{"vss"}, Live: true, Stored: true, Description: "Standard virtual switches"},
	{Name: "portgroup", Plural: "portgroups", ShortNames: []string{"pg"}, Aliases: []string{"pgs"}, Live: true, Stored: true, Description: "Standard-switch port groups, per host"},
	{Name: "vmkernel", Plural: "vmkernels", ShortNames: []string{"vmk"}, Aliases: []string{"vmks"}, Live: true, Stored: true, Description: "Host VMkernel adapters"},
	{Name: "multipath", Plural: "multipaths", ShortNames: []string{"mp"}, Live: true, Stored: true, Description: "Host/LUN multipath aggregates"},
	{Name: "event", Plural: "events", ShortNames: []string{"ev"}, Live: true, Description: "vCenter events"},
	{Name: "capture", Plural: "captures", Aliases: []string{"assessment", "assessments"}, History: true, Description: "Stored captures, the snapshots --run reads"},
	{Name: "perfcollection", Plural: "perfcollections", History: true, Description: "Stored VM performance collections, the windows top reads"},
}

// All returns every kind in display order.
func All() []Kind {
	out := make([]Kind, len(registry))
	for i, k := range registry {
		out[i] = k
		out[i].ShortNames = slices.Clone(k.ShortNames)
		out[i].Aliases = slices.Clone(k.Aliases)
	}
	return out
}

// Lookup resolves any spelling of a kind, ignoring case and surrounding
// space.
func Lookup(name string) (Kind, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, k := range All() {
		if slices.Contains(k.Names(), name) {
			return k, true
		}
	}
	return Kind{}, false
}

// Get returns the kind with the given canonical name. It is for callers
// naming a kind in code, so an unknown name is a programming error and
// returns the zero Kind.
func Get(name string) Kind {
	for _, k := range All() {
		if k.Name == name {
			return k
		}
	}
	return Kind{}
}

// ForInventory returns the kind that maps to an inventory kind.
func ForInventory(kind vsphere.Kind) (Kind, bool) {
	for _, k := range All() {
		if k.Inventory != "" && k.Inventory == kind {
			return k, true
		}
	}
	return Kind{}, false
}

// ParseInventory resolves name to one of the allowed inventory kinds. The
// error names the kinds a caller does accept, so a known kind used where it
// does not apply is told apart from a typo.
func ParseInventory(name string, allowed []vsphere.Kind) (vsphere.Kind, error) {
	k, ok := Lookup(name)
	if !ok {
		return "", fmt.Errorf("unknown kind %q (supported: %s)", name, strings.Join(InventoryNames(allowed), ", "))
	}
	if k.Inventory == "" || !slices.Contains(allowed, k.Inventory) {
		return "", fmt.Errorf("kind %q is not supported here (supported: %s)", name, strings.Join(InventoryNames(allowed), ", "))
	}
	return k.Inventory, nil
}

// InventoryNames returns the canonical names of the allowed inventory kinds,
// in the order given.
func InventoryNames(allowed []vsphere.Kind) []string {
	out := make([]string, 0, len(allowed))
	for _, vk := range allowed {
		if k, ok := ForInventory(vk); ok {
			out = append(out, k.Name)
		}
	}
	return out
}

// InventoryAliases returns every non-canonical spelling of the allowed
// inventory kinds, for shell completion's argument aliases.
func InventoryAliases(allowed []vsphere.Kind) []string {
	var out []string
	for _, vk := range allowed {
		if k, ok := ForInventory(vk); ok {
			out = append(out, k.Names()[1:]...)
		}
	}
	return out
}
