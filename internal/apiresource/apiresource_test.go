package apiresource_test

import (
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/apiresource"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestLookup(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"vm", "vm"},
		{"VMs", "vm"},
		{" virtualmachine ", "vm"},
		{"tmpl", "template"},
		{"node", "host"},
		{"nodes", "host"},
		{"esxi", "host"},
		{"cl", "cluster"},
		{"ds", "datastore"},
		{"net", "network"},
		{"dvs", "dvswitch"},
		{"dvpg", "dvportgroup"},
		{"rp", "resourcepool"},
		{"pool", "resourcepool"},
		{"snap", "snapshot"},
		{"pg", "portgroup"},
		{"portgroups", "portgroup"},
		{"vmk", "vmkernel"},
		{"ev", "event"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := apiresource.Lookup(tc.in)
			if !ok || got.Name != tc.want {
				t.Errorf("Lookup(%q) = %q, %v; want %q", tc.in, got.Name, ok, tc.want)
			}
		})
	}
	if k, ok := apiresource.Lookup("widget"); ok {
		t.Errorf("Lookup(widget) = %q; want no match", k.Name)
	}
}

// A spelling that resolved to two kinds would make the answer depend on
// registry order.
func TestEverySpellingIsUnique(t *testing.T) {
	seen := map[string]string{}
	for _, k := range apiresource.All() {
		for _, name := range k.Names() {
			if name != strings.ToLower(name) {
				t.Errorf("%s: spelling %q is not lower case, so Lookup can never match it", k.Name, name)
			}
			if prev, ok := seen[name]; ok {
				t.Errorf("spelling %q belongs to both %s and %s", name, prev, k.Name)
			}
			seen[name] = k.Name
		}
	}
}

// "portgroup" used to be an alias for network. It now names standard-switch
// port groups only, so asking for networks by it must fail rather than
// quietly return distributed port groups.
func TestPortgroupIsNotANetwork(t *testing.T) {
	if _, err := apiresource.ParseInventory("portgroup", vsphere.AllKinds); err == nil {
		t.Fatal("portgroup resolved to an inventory kind")
	}
}

func TestEveryInventoryKindIsRegistered(t *testing.T) {
	for _, vk := range append(append([]vsphere.Kind(nil), vsphere.AllKinds...), vsphere.KindResourcePool, vsphere.KindDVSwitch) {
		if _, ok := apiresource.ForInventory(vk); !ok {
			t.Errorf("inventory kind %q has no registry entry", vk)
		}
	}
}

func TestParseInventory(t *testing.T) {
	allowed := []vsphere.Kind{vsphere.KindVM, vsphere.KindHost}
	for _, tc := range []struct {
		name    string
		in      string
		want    vsphere.Kind
		wantErr string
	}{
		{name: "short name", in: "node", want: vsphere.KindHost},
		{name: "plural", in: "vms", want: vsphere.KindVM},
		{name: "typo", in: "hostz", wantErr: `unknown kind "hostz" (supported: vm, host)`},
		{name: "known but not allowed", in: "ds", wantErr: `kind "ds" is not supported here (supported: vm, host)`},
		{name: "not an inventory kind", in: "hba", wantErr: `kind "hba" is not supported here`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := apiresource.ParseInventory(tc.in, allowed)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ParseInventory(%q) error = %v; want %q", tc.in, err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("ParseInventory(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
			}
		})
	}
}
