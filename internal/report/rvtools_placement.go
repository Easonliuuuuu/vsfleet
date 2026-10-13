package report

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// vmPlacement is where RVTools places a VM in the resource hierarchy.
type vmPlacement struct {
	// resourcePool is the RVTools resource pool path: the pool's inventory
	// path without the datacenter's host folder, e.g.
	// /DC-Lab/Cluster-DR/Resources, or a vApp's path below its parent pool.
	resourcePool string
	// vApp is the name of the vApp the VM belongs to directly.
	vApp string
}

type placementKey struct{ context, vm string }

// vmPlacements indexes the resource pool and vApp collections by member VM.
// A VM missing from the index has no recorded placement: a template, a run
// that did not collect pools or vApps, or a vApp whose parent pool cannot be
// identified unambiguously.
func vmPlacements(data assessment.ExportData) map[placementKey]vmPlacement {
	type ctxPool struct {
		context string
		pool    vsphere.ResourcePool
	}
	type ctxVApp struct {
		context string
		vapp    vsphere.VApp
	}
	var pools []ctxPool
	var vapps []ctxVApp
	for _, r := range data.Resources {
		switch r.Kind {
		case "resourcepool":
			var pool vsphere.ResourcePool
			if json.Unmarshal(r.Payload, &pool) == nil {
				pools = append(pools, ctxPool{r.Context, pool})
			}
		case "vapp":
			var vapp vsphere.VApp
			if json.Unmarshal(r.Payload, &vapp) == nil {
				vapps = append(vapps, ctxVApp{r.Context, vapp})
			}
		}
	}

	// vappPath resolves a vApp below its parent pool or parent vApp. Parents
	// are recorded by name, so a parent matches only when exactly one
	// candidate in the same context, datacenter and compute resource has
	// that name; anything else is left unknown rather than guessed.
	var vappPath func(context string, v vsphere.VApp, depth int) string
	vappPath = func(context string, v vsphere.VApp, depth int) string {
		if depth > 8 {
			return ""
		}
		parent, matches := "", 0
		if v.ParentVApp != "" {
			for _, c := range vapps {
				if c.context == context && c.vapp.Name == v.ParentVApp && c.vapp.Datacenter == v.Datacenter && c.vapp.ComputeResource == v.ComputeResource {
					parent, matches = vappPath(context, c.vapp, depth+1), matches+1
				}
			}
		} else {
			for _, c := range pools {
				if c.context == context && c.pool.Name == v.ParentContainer && c.pool.Datacenter == v.Datacenter && strings.Contains(c.pool.Path+"/", "/"+v.ComputeResource+"/") {
					parent, matches = rvtoolsPoolPath(c.pool), matches+1
				}
			}
		}
		if matches != 1 || parent == "" {
			return ""
		}
		return parent + "/" + v.Name
	}

	out := make(map[placementKey]vmPlacement)
	for _, c := range pools {
		path := rvtoolsPoolPath(c.pool)
		for _, ref := range c.pool.VMRefs {
			out[placementKey{c.context, ref}] = vmPlacement{resourcePool: path}
		}
	}
	for _, c := range vapps {
		path := vappPath(c.context, c.vapp, 0)
		for _, ref := range c.vapp.DirectVMRefs {
			out[placementKey{c.context, strings.TrimPrefix(ref, "VirtualMachine:")}] = vmPlacement{resourcePool: path, vApp: c.vapp.Name}
		}
	}
	return out
}

// rvtoolsPoolPath drops the datacenter's host folder from a pool's inventory
// path: /DC-Lab/host/Cluster-DR/Resources becomes /DC-Lab/Cluster-DR/Resources.
func rvtoolsPoolPath(pool vsphere.ResourcePool) string {
	marker := "/" + pool.Datacenter + "/host/"
	if i := strings.Index(pool.Path, marker); pool.Datacenter != "" && i >= 0 {
		return pool.Path[:i] + "/" + pool.Datacenter + "/" + pool.Path[i+len(marker):]
	}
	return pool.Path
}

// rvtoolsFolder is a VM's folder as RVTools writes it: rooted at the
// datacenter (/DC-Lab, /DC-Lab/Discovered virtual machine), and empty for a
// VM inside a vApp, whose place is the vApp column instead.
func rvtoolsFolder(places map[placementKey]vmPlacement, obs assessment.Observation) any {
	vm := obs.VM
	if places[placementKey{obs.Context, vm.ID}].vApp != "" {
		return nil
	}
	if vm.Datacenter == "" || vm.Folder == "" {
		return optionalString(vm.Folder)
	}
	return "/" + vm.Datacenter + strings.TrimSuffix(vm.Folder, "/")
}

// sdkServer is the VI SDK Server value RVTools writes: the vCenter host
// (and port, when one is configured) without the URL scheme.
func sdkServer(data assessment.ExportData, context string) string {
	var endpoint string
	for _, c := range data.Contexts {
		if c.Name == context {
			endpoint = c.Endpoint
			break
		}
	}
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return endpoint
}

// deviceCount is a vInfo NICs or Disks count, empty when the run did not
// record devices: a capture before inventory schema 2, or a VM whose
// configuration vSphere did not return and that has no imported devices.
func deviceCount(data assessment.ExportData, vm vsphere.VM, n int) any {
	if !inventoryAtLeast(data.Run.InventorySchemaVersion, 2) || !vm.ConfigurationAvailable && n == 0 {
		return nil
	}
	return n
}
