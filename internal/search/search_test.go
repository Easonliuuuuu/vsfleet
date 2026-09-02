package search_test

import (
	"testing"

	"github.com/easonliuuuuu/vc-tui/internal/search"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

func inventory() *vsphere.Inventory {
	loc := func(dc, path string) vsphere.Location {
		return vsphere.Location{Context: "prod", Datacenter: dc, Path: path}
	}
	return &vsphere.Inventory{
		Context: "prod",
		VMs: []vsphere.VM{
			{Location: loc("Taipei", "/Taipei/vm/ubuntu-24-build-17"), Name: "ubuntu-24-build-17", PowerState: "poweredOn"},
			{Location: loc("Taipei", "/Taipei/vm/windows-builder"), Name: "windows-builder", PowerState: "poweredOff"},
		},
		Templates: []vsphere.VM{
			{Location: loc("Taipei", "/Taipei/vm/Templates/ubuntu-24.04-v8"), Name: "ubuntu-24.04-v8", IsTemplate: true, GuestOS: "Ubuntu 24.04"},
		},
		Datastores: []vsphere.Datastore{
			{Location: loc("Taipei", "/Taipei/datastore/ubuntu-images"), Name: "ubuntu-images", Type: "NFS"},
		},
	}
}

func TestMatchIsCaseInsensitiveSubstring(t *testing.T) {
	got := search.Match(inventory(), "UBUNTU-24", search.Options{})
	if len(got) != 0 {
		t.Fatalf("the needle is expected to arrive lower-cased, got %d matches", len(got))
	}
	got = search.Match(inventory(), "ubuntu-24", search.Options{})
	if len(got) != 2 {
		t.Fatalf("expected 2 matches, got %d: %+v", len(got), got)
	}
	for _, m := range got {
		if m.Context != "prod" {
			t.Errorf("match %q is not attributed to its vCenter", m.Name)
		}
		if m.Path == "" {
			t.Errorf("match %q has no inventory path", m.Name)
		}
	}
}

func TestMatchRestrictsKinds(t *testing.T) {
	got := search.Match(inventory(), "ubuntu", search.Options{Kinds: []vsphere.Kind{vsphere.KindTemplate}})
	if len(got) != 1 || got[0].Kind != vsphere.KindTemplate {
		t.Fatalf("expected only the template, got %+v", got)
	}
	if got[0].Description != "Ubuntu 24.04" {
		t.Errorf("template description is %q", got[0].Description)
	}
}

func TestMatchEmptyQueryReturnsEverything(t *testing.T) {
	got := search.Match(inventory(), "", search.Options{})
	if len(got) != 4 {
		t.Fatalf("expected the whole inventory, got %d", len(got))
	}
}

func TestParseKind(t *testing.T) {
	for in, want := range map[string]vsphere.Kind{
		"vm":         vsphere.KindVM,
		"vms":        vsphere.KindVM,
		"templates":  vsphere.KindTemplate,
		"esxi":       vsphere.KindHost,
		"ds":         vsphere.KindDatastore,
		"portgroups": vsphere.KindNetwork,
	} {
		got, err := vsphere.ParseKind(in)
		if err != nil || got != want {
			t.Errorf("ParseKind(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := vsphere.ParseKind("resourcepool"); err == nil {
		t.Error("expected an unknown kind to be rejected")
	}
}
