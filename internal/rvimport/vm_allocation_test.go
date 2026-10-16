package rvimport

import (
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// The "lab" cells are those of vm-reservations in the real RVTools 4.8.1.4
// export of the nested lab.
func TestVMAllocationIsReadBack(t *testing.T) {
	path := writeFixtureWorkbook(t, nil)
	no := false
	cpuReservation, cpuLimit, memReservation, memLimit := int64(500), int64(1000), int64(128), int64(256)
	for _, tc := range []struct {
		name                 string
		cpu, memory          map[string]string
		wantSockets, wantCPS int32
		wantCPU, wantMemory  *vsphere.VMResourceAllocation
		wantLocked           *bool
	}{
		{"lab",
			map[string]string{"Sockets": "2", "Cores p/s": "2", "Reservation": "500", "Limit": "1000"},
			map[string]string{"Memory Reservation Locked To Max": "False", "Reservation": "128", "Limit": "256"},
			2, 2, &vsphere.VMResourceAllocation{Reservation: &cpuReservation, Limit: &cpuLimit},
			&vsphere.VMResourceAllocation{Reservation: &memReservation, Limit: &memLimit}, &no},
		{"not reported",
			map[string]string{"Sockets": "", "Cores p/s": "", "Reservation": "", "Limit": ""},
			map[string]string{"Memory Reservation Locked To Max": "", "Reservation": "", "Limit": ""},
			0, 0, nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openFixture(t, path)
			for header, value := range tc.cpu {
				setColumnValue(t, f, sheetVCPU, header, vmRow(t, f, sheetVCPU, "web-01"), value)
			}
			for header, value := range tc.memory {
				setColumnValue(t, f, sheetVMemory, header, vmRow(t, f, sheetVMemory, "web-01"), value)
			}
			result, err := Parse(f, Options{})
			if err != nil {
				t.Fatal(err)
			}
			vm := findVM(result, "alpha", "web-01")
			if vm.CPUSockets != tc.wantSockets || vm.CoresPerSocket != tc.wantCPS {
				t.Errorf("sockets, cores p/s = %d, %d, want %d, %d", vm.CPUSockets, vm.CoresPerSocket, tc.wantSockets, tc.wantCPS)
			}
			if !reflect.DeepEqual(vm.CPUAllocation, tc.wantCPU) || !reflect.DeepEqual(vm.MemoryAllocation, tc.wantMemory) {
				t.Errorf("cpu, memory allocation = %+v, %+v, want %+v, %+v", vm.CPUAllocation, vm.MemoryAllocation, tc.wantCPU, tc.wantMemory)
			}
			if !reflect.DeepEqual(vm.MemoryReservationLockedToMax, tc.wantLocked) {
				t.Errorf("locked to max = %v, want %v", vm.MemoryReservationLockedToMax, tc.wantLocked)
			}
		})
	}
}

// The cells are those of Cluster-DR (DRS on), Cluster-Lab (DRS off) and
// local-esxi01 in the real RVTools 4.8.1.4 export of the nested lab.
func TestClusterCapacityAndDatastoreURLAreReadBack(t *testing.T) {
	path := writeFixtureWorkbook(t, nil)
	const url = "ds:///vmfs/volumes/6ab8f549-3dc1bcad-5f72-525400865436/"
	for _, tc := range []struct {
		name, drs, effectiveCPU, effectiveMemory, url string
		wantCPU, wantMemory                           int64
		wantBehavior                                  string
	}{
		{"DRS on", "True", "7263", "5460", url, 7263, 5460, "fullyAutomated"},
		// A live capture keeps no automation level while DRS is off.
		{"DRS off", "False", "6888", "15158", url, 6888, 15158, ""},
		{"not reported", "True", "", "", "", 0, 0, "fullyAutomated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openFixture(t, path)
			setColumnValue(t, f, sheetVCluster, "DRS enabled", 2, tc.drs)
			setColumnValue(t, f, sheetVCluster, "DRS default VM behavior", 2, "fullyAutomated")
			setColumnValue(t, f, sheetVCluster, "Effective Cpu", 2, tc.effectiveCPU)
			setColumnValue(t, f, sheetVCluster, "Effective Memory", 2, tc.effectiveMemory)
			setColumnValue(t, f, sheetVDatastore, "URL", 2, tc.url)
			result, err := Parse(f, Options{})
			if err != nil {
				t.Fatal(err)
			}
			cl := findCluster(result, "alpha", "cluster-a")
			if cl == nil {
				t.Fatal("cluster-a not imported")
			}
			if cl.EffectiveCPUMHz != tc.wantCPU || cl.EffectiveMemoryMB != tc.wantMemory || cl.DRSBehavior != tc.wantBehavior {
				t.Errorf("effective cpu, memory, DRS behavior = %d, %d, %q, want %d, %d, %q",
					cl.EffectiveCPUMHz, cl.EffectiveMemoryMB, cl.DRSBehavior, tc.wantCPU, tc.wantMemory, tc.wantBehavior)
			}
			if ds := findDatastore(result, "alpha", "nvme-01"); ds == nil || ds.Backing.URL != tc.url {
				t.Errorf("datastore = %+v, want URL %q", ds, tc.url)
			}
		})
	}
}

// vmRow returns the 1-based worksheet row of the first row for the named VM.
func vmRow(t *testing.T, f *excelize.File, sheet, name string) int {
	t.Helper()
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range rows[1:] {
		if len(row) > 0 && row[0] == name {
			return i + 2
		}
	}
	t.Fatalf("%s has no row for %q", sheet, name)
	return 0
}

func findDatastore(result *Result, contextName, name string) *vsphere.Datastore {
	for _, c := range result.contexts {
		if c.name != contextName {
			continue
		}
		for _, ds := range c.datastores {
			if ds.Name == name {
				return ds
			}
		}
	}
	return nil
}
