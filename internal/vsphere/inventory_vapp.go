package vsphere

import (
	"context"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// linkVApps records where vApps and their members sit in the VM folder tree,
// which their parent property alone does not say.
//
// A VM inside a vApp has no parent at all: vSphere carries the link only as
// the VM's parentVApp, so without this its path stops at the VM itself and it
// resolves to no datacenter and the root folder. A vApp's own parent is the
// resource pool it draws from, on the compute side of the inventory, while
// the vSphere client and govc place it in the VM folder tree through
// parentFolder (a top-level vApp) or parentVApp (a nested one). That
// folder-side parent goes in folderParent, which inventory paths follow;
// placement lookups keep walking the compute side through parent.
func (i *index) linkVApps(vapps []mo.VirtualApp) {
	for _, v := range vapps {
		switch {
		case v.ParentFolder != nil:
			i.folderParent[v.Self] = *v.ParentFolder
		case v.ParentVApp != nil:
			i.folderParent[v.Self] = *v.ParentVApp
		}
		for _, ref := range v.Vm {
			if e, ok := i.byRef[ref]; ok && e.parent == nil {
				self := v.Self
				e.parent = &self
				i.byRef[ref] = e
			}
		}
	}
}

// retrieveVAppLinks reads the folder-side links linkVApps needs. Only vApps
// are read, so the cost follows the number of vApps rather than of VMs.
func retrieveVAppLinks(ctx context.Context, c *Client, root types.ManagedObjectReference) ([]mo.VirtualApp, error) {
	var vapps []mo.VirtualApp
	err := retrieve(ctx, c, root, []string{"VirtualApp"}, []string{"VirtualApp"}, []string{"parentFolder", "parentVApp", "vm"}, &vapps)
	return vapps, err
}
