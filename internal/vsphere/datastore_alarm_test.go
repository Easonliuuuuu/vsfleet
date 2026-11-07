package vsphere_test

import (
	"context"
	"testing"

	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/types"
)

func TestListDatastoresReadsTriggeredAlarms(t *testing.T) {
	model := simulator.VPX()
	model.Datastore = 2
	if err := model.Create(); err != nil {
		t.Fatalf("create simulator model: %v", err)
	}
	t.Cleanup(model.Remove)

	alarms := model.Map().AllReference("Alarm")
	if len(alarms) == 0 {
		t.Fatal("simulator has no default alarms")
	}
	alarm := alarms[0].Reference()
	name := model.Map().Get(alarm).(*simulator.Alarm).Info.Name
	ds := model.Map().Any("Datastore").(*simulator.Datastore)
	ds.TriggeredAlarmState = []types.AlarmState{
		{Key: "a1", Alarm: alarm, Entity: ds.Self, OverallStatus: types.ManagedEntityStatusYellow},
		{Key: "a2", Alarm: alarm, Entity: ds.Self, OverallStatus: types.ManagedEntityStatusRed},
		{Key: "a3", Alarm: alarm, Entity: ds.Self, OverallStatus: types.ManagedEntityStatusGray},
	}

	gc, endpoint := dialSimulator(t, model)
	c := clientFor(gc, endpoint, "")
	datastores, err := c.ListDatastores(context.Background())
	if err != nil {
		t.Fatalf("ListDatastores: %v", err)
	}
	found := false
	for _, d := range datastores {
		if !d.AlarmsRead {
			t.Errorf("%s: alarms not marked read", d.Name)
		}
		if d.ID != ds.Self.Value {
			if len(d.Alarms) != 0 {
				t.Errorf("%s: alarms = %+v, want none", d.Name, d.Alarms)
			}
			continue
		}
		found = true
		if len(d.Alarms) != 2 || d.Alarms[0].Status != "red" || d.Alarms[1].Status != "yellow" {
			t.Fatalf("alarms = %+v, want the red one then the yellow one", d.Alarms)
		}
		if a := d.Alarms[0]; a.Name != name || a.Entity != ds.Name || a.EntityType != "Datastore" {
			t.Errorf("alarm = %+v, want %q on %s", a, name, ds.Name)
		}
	}
	if !found {
		t.Fatalf("datastore %s not listed", ds.Self.Value)
	}
}
