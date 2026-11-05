package report

import (
	"encoding/json"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// resourcePoolHeaders follow RVTools' vRP order. vCPUs, Config status and
// Datacenter are vsfleet's own, after the RVTools column they extend.
var resourcePoolHeaders = []string{"Resource Pool name", "Resource Pool path", "Status", "# VMs total", "# VMs", "# vCPUs", "vCPUs",
	"CPU limit", "CPU overheadLimit", "CPU reservation", "CPU level", "CPU shares", "CPU expandableReservation", "Mem Configured",
	"Mem limit", "Mem overheadLimit", "Mem reservation", "Mem level", "Mem shares", "Mem expandableReservation", "Config status",
	"Object ID", "Datacenter", "VI SDK Server", "VI SDK UUID", "vsfleet Context"}

// poolMember is what vRP counts of one VM.
type poolMember struct {
	cpus      int32
	poweredOn bool
}

func resourcePoolRows(data assessment.ExportData) [][]any {
	members := make(map[placementKey]poolMember, len(data.VMs))
	for _, item := range data.VMs {
		vm := item.Observation.VM
		members[placementKey{item.Observation.Context, vm.ID}] = poolMember{cpus: vm.CPU, poweredOn: vm.PowerState == "poweredOn" && !vm.IsTemplate}
	}
	h := newPoolHierarchy(data)
	rows := make([][]any, 0)
	for _, r := range data.Resources {
		switch r.Kind {
		case "resourcepool":
			var pool vsphere.ResourcePool
			if err := json.Unmarshal(r.Payload, &pool); err != nil {
				continue
			}
			rows = append(rows, resourcePoolRow(data, r, members, nonempty(pool.Name, r.Name), rvtoolsPoolPath(pool), pool.Status, pool.VMRefs,
				pool.ResourceAllocation, pool.ConfigStatus, nonempty(pool.ID, r.ID), pool.Datacenter))
		case "vapp":
			// RVTools lists vApps on vRP: a vApp is a resource pool subtype
			// with the same allocation. Its VMs are its direct members.
			var vapp vsphere.VApp
			if err := json.Unmarshal(r.Payload, &vapp); err != nil || vapp.Allocation == nil {
				continue
			}
			refs := make([]string, len(vapp.DirectVMRefs))
			for i, ref := range vapp.DirectVMRefs {
				refs[i] = strings.TrimPrefix(ref, "VirtualMachine:")
			}
			rows = append(rows, resourcePoolRow(data, r, members, nonempty(vapp.Name, r.Name), h.vappPath(r.Context, vapp), vapp.OverallStatus, refs,
				*vapp.Allocation, vapp.ConfigStatus, nonempty(vapp.ID, r.ID), vapp.Datacenter))
		}
	}
	return rows
}

// resourcePoolRow renders one vRP row. # VMs and # vCPUs count the pool's
// powered-on VMs, as RVTools does, and are empty unless the context's VM
// collection completed; vCPUs counts every VM in the pool.
func resourcePoolRow(data assessment.ExportData, r assessment.ResourceObservation, members map[placementKey]poolMember, name, path, status string,
	vmRefs []string, a vsphere.ResourceAllocation, configStatus, id, datacenter string) []any {
	var vCPUs, onVMs, onCPUs int32
	for _, ref := range vmRefs {
		m := members[placementKey{r.Context, ref}]
		vCPUs += m.cpus
		if m.poweredOn {
			onVMs++
			onCPUs += m.cpus
		}
	}
	var poweredVMs, poweredCPUs any
	if vmCollectionComplete(data, r.Context) {
		poweredVMs, poweredCPUs = onVMs, onCPUs
	}
	return []any{
		name, optionalString(path), status, len(vmRefs), poweredVMs, poweredCPUs, vCPUs,
		optionalInt64(a.CPULimitMHz), optionalInt64(a.CPUOverheadLimitMHz), optionalInt64(a.CPUReservationMHz),
		a.CPULevel, a.CPUShares, a.CPUExpandable, a.MemConfiguredMB,
		optionalInt64(a.MemLimitMB), optionalInt64(a.MemOverheadLimitMB), optionalInt64(a.MemReservationMB),
		a.MemLevel, a.MemShares, a.MemExpandable, configStatus,
		id, datacenter, sdkServer(data, r.Context), r.VCenterID, r.Context,
	}
}
