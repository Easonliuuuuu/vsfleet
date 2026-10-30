package assessment

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func hiddenCollection(t *testing.T, kind string, values ...any) CollectionResult {
	t.Helper()
	c := CollectionResult{Kind: kind, Status: "success", ItemCount: len(values)}
	if len(values) == 0 {
		c.Status = "empty"
	}
	for _, v := range values {
		payload, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		c.Resources = append(c.Resources, ResourceObservation{Kind: kind, Payload: payload})
	}
	return c
}

func hiddenStatus(r ContextResult, kind string) (string, string) {
	for _, c := range r.Collections {
		if c.Kind == kind {
			return c.Status, c.Error
		}
	}
	return "", ""
}

func TestMarkHiddenInventoryDemotesContradictedCollections(t *testing.T) {
	nic := vsphere.VMNIC{Network: "dvpg-vlan200", NetworkID: "dvportgroup-1005", SwitchID: "50 aa"}
	vm := vsphere.VM{Name: "app", PowerState: "poweredOn", Host: "esx-02", NICs: []vsphere.VMNIC{nic}}
	r := ContextResult{
		VMs: []Observation{{VM: vm}},
		Collections: []CollectionResult{
			hiddenCollection(t, "host"),
			hiddenCollection(t, "cluster", vsphere.Cluster{Name: "Cluster-DR", Hosts: 1}),
			hiddenCollection(t, "network"),
			hiddenCollection(t, "dvswitch"),
		},
	}
	markHiddenInventory(&r, nil)
	for _, kind := range []string{"host", "network", "dvswitch"} {
		status, reason := hiddenStatus(r, kind)
		if status != CollectionPartial || !strings.Contains(reason, "permission-hidden") {
			t.Errorf("%s = %q (%q), want partial with a reason", kind, status, reason)
		}
	}
	if status, _ := hiddenStatus(r, "cluster"); status != "success" {
		t.Errorf("cluster = %q, want untouched", status)
	}
	var runs []CollectionRun
	for _, c := range r.Collections {
		runs = append(runs, CollectionRun{Kind: c.Kind, Status: c.Status})
	}
	if ContextComplete(ContextRun{Collections: runs}, []string{"host", "dvswitch"}) {
		t.Error("a demoted collection still counts as complete evidence")
	}
}

func TestMarkHiddenInventoryLeavesConsistentInventoryAlone(t *testing.T) {
	vm := vsphere.VM{Name: "app", PowerState: "poweredOn", Host: "esx-01", NICs: []vsphere.VMNIC{
		{Network: "dvpg", NetworkID: "dvportgroup-1", SwitchID: "50-AA"},
		{Network: "nsx-seg", NetworkID: "opaque-segment-uuid"},
	}}
	r := ContextResult{
		VMs: []Observation{{VM: vm}},
		Collections: []CollectionResult{
			hiddenCollection(t, "host", vsphere.Host{Name: "esx-01", Cluster: "C1"}),
			hiddenCollection(t, "cluster", vsphere.Cluster{Name: "C1", Hosts: 1}),
			hiddenCollection(t, "network", vsphere.Network{ID: "dvportgroup-1", Name: "dvpg"}),
			hiddenCollection(t, "dvswitch", vsphere.DVSwitch{UUID: "50-aa"}),
		},
	}
	markHiddenInventory(&r, nil)
	for _, c := range r.Collections {
		if c.Status != "success" {
			t.Errorf("%s = %q (%s), want success", c.Kind, c.Status, c.Error)
		}
	}
}

// A live listing gets the capture's verdict: a VM using what was not returned
// contradicts it, unless vCenter confirmed the network was deleted.
func TestHiddenInListingChecksOneKind(t *testing.T) {
	vms := []vsphere.VM{{Name: "app", NICs: []vsphere.VMNIC{{NetworkID: "dvportgroup-1005", SwitchID: "50 aa"}}}}
	nothingDeleted := func([]string) map[string]bool { return nil }
	for _, tc := range []struct {
		name    string
		reason  string
		deleted func([]string) map[string]bool
		got     func(confirm func([]string) map[string]bool) string
	}{
		{
			name: "no networks returned", reason: `permission-hidden inventory: VM "app" uses network "" (dvportgroup-1005)`, deleted: nothingDeleted,
			got: func(c func([]string) map[string]bool) string {
				return hiddenInListing("network", []vsphere.Network{}, vms, c)
			},
		},
		{
			name: "the network was deleted", deleted: func(ids []string) map[string]bool {
				out := map[string]bool{}
				for _, id := range ids {
					out[id] = true
				}
				return out
			},
			got: func(c func([]string) map[string]bool) string {
				return hiddenInListing("network", []vsphere.Network{}, vms, c)
			},
		},
		{
			name: "the network was returned", deleted: nothingDeleted,
			got: func(c func([]string) map[string]bool) string {
				return hiddenInListing("network", []vsphere.Network{{ID: "dvportgroup-1005"}}, vms, c)
			},
		},
		{
			name: "no switches returned", reason: `permission-hidden inventory: VM "app" uses distributed switch 50 aa`, deleted: nothingDeleted,
			got: func(c func([]string) map[string]bool) string {
				return hiddenInListing("dvswitch", []vsphere.DVSwitch{}, vms, c)
			},
		},
		{
			name: "the switch was returned", deleted: nothingDeleted,
			got: func(c func([]string) map[string]bool) string {
				return hiddenInListing("dvswitch", []vsphere.DVSwitch{{UUID: "50 AA"}}, vms, c)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.got(tc.deleted)
			if tc.reason == "" && got != "" || tc.reason != "" && !strings.Contains(got, tc.reason) {
				t.Fatalf("reason = %q, want %q", got, tc.reason)
			}
		})
	}
}
