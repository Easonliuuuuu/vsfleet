package vsphere

import (
	"testing"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/easonliuuuuu/vsfleet/internal/config"
)

// vappIndex builds the shape vCenter reports for vApps: each vApp's parent is
// on the compute side, its folder is parentFolder or parentVApp, and its
// member VMs have no parent at all.
func vappIndex(t *testing.T) (*index, map[string]types.ManagedObjectReference) {
	t.Helper()
	refs := map[string]types.ManagedObjectReference{
		"dc":     {Type: "Datacenter", Value: "datacenter-1"},
		"vmRoot": {Type: "Folder", Value: "group-v1"},
		"apps":   {Type: "Folder", Value: "group-v2"},
		"hosts":  {Type: "Folder", Value: "group-h1"},
		"cr":     {Type: "ClusterComputeResource", Value: "domain-c1"},
		"pool":   {Type: "ResourcePool", Value: "resgroup-1"},
		"outer":  {Type: "VirtualApp", Value: "resgroup-v1"},
		"inner":  {Type: "VirtualApp", Value: "resgroup-v2"},
		"api":    {Type: "VirtualMachine", Value: "vm-1"},
		"db":     {Type: "VirtualMachine", Value: "vm-2"},
		"plain":  {Type: "VirtualMachine", Value: "vm-3"},
	}
	ref := func(name string) *types.ManagedObjectReference {
		r := refs[name]
		return &r
	}
	idx := &index{
		byRef: map[types.ManagedObjectReference]entity{
			refs["dc"]:     {ref: refs["dc"], name: "DC0"},
			refs["vmRoot"]: {ref: refs["vmRoot"], name: "vm", parent: ref("dc")},
			refs["apps"]:   {ref: refs["apps"], name: "Apps", parent: ref("vmRoot")},
			refs["hosts"]:  {ref: refs["hosts"], name: "host", parent: ref("dc")},
			refs["cr"]:     {ref: refs["cr"], name: "compute-a", parent: ref("hosts")},
			refs["pool"]:   {ref: refs["pool"], name: "Resources", parent: ref("cr")},
			refs["outer"]:  {ref: refs["outer"], name: "web-stack", parent: ref("pool")},
			refs["inner"]:  {ref: refs["inner"], name: "web-cache", parent: ref("outer")},
			refs["api"]:    {ref: refs["api"], name: "api-01"},
			refs["db"]:     {ref: refs["db"], name: "db-01"},
			refs["plain"]:  {ref: refs["plain"], name: "jump-01", parent: ref("vmRoot")},
		},
		folderParent: map[types.ManagedObjectReference]types.ManagedObjectReference{},
	}
	outer := mo.VirtualApp{ParentFolder: ref("apps")}
	outer.Self, outer.Vm = refs["outer"], []types.ManagedObjectReference{refs["api"]}
	inner := mo.VirtualApp{ParentVApp: ref("outer")}
	inner.Self, inner.Vm = refs["inner"], []types.ManagedObjectReference{refs["db"]}
	idx.linkVApps([]mo.VirtualApp{outer, inner})
	return idx, refs
}

func TestVMInsideVAppResolvesThroughItsVApp(t *testing.T) {
	idx, refs := vappIndex(t)
	c := &Client{Context: &config.Context{Name: "prod"}}
	cases := []struct {
		name       string
		vm         string
		parent     *types.ManagedObjectReference
		parentVApp string
		wantPath   string
		wantFolder string
	}{
		{name: "member of a top-level vApp", vm: "api", parentVApp: "outer", wantPath: "/DC0/vm/Apps/web-stack/api-01", wantFolder: "/Apps/web-stack"},
		{name: "member of a nested vApp", vm: "db", parentVApp: "inner", wantPath: "/DC0/vm/Apps/web-stack/web-cache/db-01", wantFolder: "/Apps/web-stack/web-cache"},
		{name: "VM in the root folder", vm: "plain", parent: &types.ManagedObjectReference{Type: "Folder", Value: "group-v1"}, wantPath: "/DC0/vm/jump-01", wantFolder: "/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &mo.VirtualMachine{}
			m.Self, m.Name, m.Parent = refs[tc.vm], idx.byRef[refs[tc.vm]].name, tc.parent
			if tc.parentVApp != "" {
				r := refs[tc.parentVApp]
				m.ParentVApp = &r
			}
			vm := newVM(c, idx, m)
			if vm.Path != tc.wantPath || vm.Folder != tc.wantFolder || vm.Datacenter != "DC0" {
				t.Errorf("path, folder, datacenter = %q, %q, %q; want %q, %q, DC0", vm.Path, vm.Folder, vm.Datacenter, tc.wantPath, tc.wantFolder)
			}
		})
	}
}

// A VM created after the index was built is not in it; its parentVApp still
// places it, the way a parent folder places an ordinary new VM.
func TestNewVAppMemberMissingFromIndexResolvesThroughParentVApp(t *testing.T) {
	idx, refs := vappIndex(t)
	outer := refs["outer"]
	m := &mo.VirtualMachine{ParentVApp: &outer}
	m.Self, m.Name = types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-9"}, "api-02"
	vm := newVM(&Client{Context: &config.Context{Name: "prod"}}, idx, m)
	if vm.Path != "/DC0/vm/Apps/web-stack/api-02" || vm.Folder != "/Apps/web-stack" || vm.Datacenter != "DC0" {
		t.Errorf("path, folder, datacenter = %q, %q, %q", vm.Path, vm.Folder, vm.Datacenter)
	}
}

// Placement still follows the compute side: the folder-side link changes a
// vApp's path, not the cluster it runs on.
func TestVAppKeepsComputePlacementWithFolderPath(t *testing.T) {
	idx, refs := vappIndex(t)
	raw := &mo.VirtualApp{}
	raw.Self = refs["outer"]
	raw.Name = "web-stack"
	pool := refs["pool"]
	raw.Parent = &pool
	got := newVApp(&Client{Context: &config.Context{Name: "prod"}}, idx, raw)
	if got.Path != "/DC0/vm/Apps/web-stack" || got.Cluster != "compute-a" || got.ComputeResource != "compute-a" || got.Datacenter != "DC0" {
		t.Errorf("path, cluster, compute resource, datacenter = %q, %q, %q, %q", got.Path, got.Cluster, got.ComputeResource, got.Datacenter)
	}
}
