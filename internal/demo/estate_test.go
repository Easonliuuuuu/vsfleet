package demo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestEstateIsDeterministic(t *testing.T) {
	a, _ := json.Marshal(NewBackend().inventories)
	b, _ := json.Marshal(NewBackend().inventories)
	if string(a) != string(b) {
		t.Fatal("two demo backends produced different inventories")
	}
}

func TestProdEstateIsProductionSized(t *testing.T) {
	inv := NewBackend().inventories["prod-vc"]
	for name, got := range map[string][3]int{
		"vms":        {len(inv.VMs), 1000, 1000},
		"datastores": {len(inv.Datastores), 30, 60},
		"vapps":      {len(inv.VApps), 20, 60},
		"hosts":      {len(inv.Hosts), 50, 100},
		"clusters":   {len(inv.Clusters), 5, 10},
		"networks":   {len(inv.Networks), 25, 80},
		"templates":  {len(inv.Templates), 10, 40},
	} {
		if got[0] < got[1] || got[0] > got[2] {
			t.Errorf("%s = %d, want within [%d,%d]", name, got[0], got[1], got[2])
		}
	}
}

// Every reference in the generated estate must resolve; the old hand-written
// fixture put a VM on a host that did not exist.
func TestEstateReferentialIntegrity(t *testing.T) {
	b := NewBackend()
	for ctx, inv := range b.inventories {
		hosts, clusters, stores, vmIDs, poolIDs, vapps := map[string]*vsphere.Host{}, map[string]*vsphere.Cluster{}, map[string]*vsphere.Datastore{}, map[string]bool{}, map[string]bool{}, map[string]*vsphere.VApp{}
		for i := range inv.Hosts {
			hosts[inv.Hosts[i].Name] = &inv.Hosts[i]
		}
		for i := range inv.Clusters {
			clusters[inv.Clusters[i].Name] = &inv.Clusters[i]
		}
		for i := range inv.Datastores {
			stores[inv.Datastores[i].Name] = &inv.Datastores[i]
		}
		for i := range inv.ResourcePools {
			poolIDs[inv.ResourcePools[i].ID] = true
		}
		for i := range inv.VApps {
			vapps[inv.VApps[i].Name] = &inv.VApps[i]
		}
		placed := map[string]int{}
		names := map[string]bool{}
		for _, vm := range inv.VMs {
			if names[vm.Name] {
				t.Errorf("%s: duplicate VM name %q", ctx, vm.Name)
			}
			names[vm.Name] = true
			vmIDs[vm.ID] = true
			if hosts[vm.Host] == nil || clusters[vm.Cluster] == nil || hosts[vm.Host].Cluster != vm.Cluster {
				t.Fatalf("%s: VM %s host %q cluster %q do not resolve", ctx, vm.Name, vm.Host, vm.Cluster)
			}
			placed[vm.Host]++
			for _, d := range vm.Datastores {
				if stores[d] == nil {
					t.Fatalf("%s: VM %s uses missing datastore %q", ctx, vm.Name, d)
				}
			}
			for _, d := range vm.Disks {
				if n, _, ok := vsphere.SplitDatastorePath(d.BackingPath); !ok || stores[n] == nil {
					t.Fatalf("%s: VM %s disk path %q does not resolve", ctx, vm.Name, d.BackingPath)
				}
			}
		}
		for name, h := range hosts {
			if h.VMCount != placed[name] {
				t.Errorf("%s: host %s VMCount=%d, placed %d", ctx, name, h.VMCount, placed[name])
			}
		}
		for name, c := range clusters {
			n, cores := 0, int32(0)
			for _, h := range hosts {
				if h.Cluster == name {
					n++
					cores += h.CPUCores
				}
			}
			if c.Hosts != n || c.CPUCores != cores {
				t.Errorf("%s: cluster %s totals hosts=%d cores=%d, want %d/%d", ctx, name, c.Hosts, c.CPUCores, n, cores)
			}
		}
		for name, ds := range stores {
			if ds.FreeBytes < 0 || ds.FreeBytes > ds.CapacityBytes {
				t.Errorf("%s: datastore %s free=%d capacity=%d", ctx, name, ds.FreeBytes, ds.CapacityBytes)
			}
		}
		owned := map[string]string{}
		for _, v := range inv.VApps {
			for i, ref := range v.DirectVMRefs {
				id := strings.TrimPrefix(ref, "VirtualMachine:")
				if !vmIDs[id] {
					t.Errorf("%s: vApp %s member %s missing", ctx, v.Name, ref)
				}
				if prev := owned[id]; prev != "" {
					t.Errorf("%s: VM %s is in vApps %s and %s", ctx, v.DirectVMs[i], prev, v.Name)
				}
				owned[id] = v.Name
			}
			for _, ref := range v.ChildResourcePoolRefs {
				if !poolIDs[strings.TrimPrefix(ref, "ResourcePool:")] {
					t.Errorf("%s: vApp %s child pool %s missing", ctx, v.Name, ref)
				}
			}
			for depth, cur := 0, v.ParentVApp; cur != ""; depth++ {
				if depth > 8 || vapps[cur] == nil {
					t.Fatalf("%s: vApp %s has a cyclic or dangling parent chain", ctx, v.Name)
				}
				cur = vapps[cur].ParentVApp
			}
		}
		for _, p := range inv.ResourcePools {
			for _, ref := range p.VMRefs {
				if !vmIDs[ref] {
					t.Errorf("%s: pool %s member %s missing", ctx, p.Name, ref)
				}
				if owned[ref] != "" {
					t.Errorf("%s: pool %s lists vApp-owned VM %s", ctx, p.Name, ref)
				}
			}
		}
	}
}

func TestSharedNamesAcrossContexts(t *testing.T) {
	b := NewBackend()
	for _, name := range []string{"api-01", "postgres-01"} {
		for _, ctx := range []string{"prod-vc", "edge-vc"} {
			found := false
			for _, vm := range b.inventories[ctx].VMs {
				found = found || vm.Name == name
			}
			if !found {
				t.Errorf("%s has no %s; the duplicate-names scenario needs the collision", ctx, name)
			}
		}
	}
}

func TestHistoryHasFiveRunsThatDiffer(t *testing.T) {
	start := time.Now()
	service, closeStore, err := NewBackend().AssessmentService()
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore()
	t.Logf("seeded history in %s", time.Since(start))
	runs, err := service.Store.Runs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 5 {
		t.Fatalf("runs=%d, want 5", len(runs))
	}
	first, last := runs[len(runs)-1], runs[0]
	if first.ID == last.ID {
		t.Fatal("expected distinct runs")
	}
}
