package vsphere_test

import (
	"context"
	"testing"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/simulator"

	"github.com/easonliuuuuu/vc-tui/internal/config"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

// newSimulator starts a vcsim vCenter and returns a client wired to it.
func newSimulator(t *testing.T, tune func(*simulator.Model)) *vsphere.Client {
	t.Helper()
	model := simulator.VPX()
	if tune != nil {
		tune(model)
	}
	if err := model.Create(); err != nil {
		t.Fatalf("create simulator model: %v", err)
	}
	t.Cleanup(model.Remove)

	server := model.Service.NewServer()
	t.Cleanup(server.Close)

	ctx := context.Background()
	gc, err := govmomi.NewClient(ctx, server.URL, true)
	if err != nil {
		t.Fatalf("connect to simulator: %v", err)
	}
	t.Cleanup(func() { _ = gc.Logout(context.Background()) })

	cc := &config.Context{
		Name:     "sim",
		Endpoint: server.URL.String(),
		Username: "user",
		TLS:      config.TLSConfig{Mode: config.TLSInsecure},
	}
	cc.Normalize()
	return vsphere.NewClientForTest(cc, gc)
}

func TestListInventory(t *testing.T) {
	c := newSimulator(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 2
		m.ClusterHost = 3
		m.Machine = 4
		m.Datastore = 2
	})
	ctx := context.Background()

	inv, err := c.ListInventory(ctx)
	if err != nil {
		t.Fatalf("ListInventory: %v", err)
	}
	t.Logf("inventory: %s", inv.Counts())

	if len(inv.VMs) == 0 {
		t.Fatal("expected virtual machines")
	}
	if len(inv.Hosts) == 0 {
		t.Fatal("expected hosts")
	}
	if len(inv.Clusters) == 0 {
		t.Fatal("expected clusters")
	}
	if len(inv.Datastores) == 0 {
		t.Fatal("expected datastores")
	}
	if len(inv.Networks) == 0 {
		t.Fatal("expected networks")
	}

	vm := inv.VMs[0]
	t.Logf("vm: %+v", vm)
	if vm.Name == "" || vm.PowerState == "" {
		t.Errorf("vm is missing basic properties: %+v", vm)
	}
	if vm.Datacenter == "" {
		t.Errorf("vm %s has no datacenter", vm.Name)
	}
	if vm.Host == "" {
		t.Errorf("vm %s has no host", vm.Name)
	}
	if vm.Cluster == "" {
		t.Errorf("vm %s has no cluster", vm.Name)
	}
	if vm.CPU == 0 || vm.MemoryMB == 0 {
		t.Errorf("vm %s has no hardware: cpu=%d mem=%d", vm.Name, vm.CPU, vm.MemoryMB)
	}
	if vm.Path == "" {
		t.Errorf("vm %s has no inventory path", vm.Name)
	}

	host := inv.Hosts[0]
	t.Logf("host: %+v", host)
	if host.Cluster == "" {
		t.Errorf("host %s has no cluster", host.Name)
	}
	if host.CPUCores == 0 || host.MemoryMB == 0 {
		t.Errorf("host %s has no hardware summary: %+v", host.Name, host)
	}

	cl := inv.Clusters[0]
	t.Logf("cluster: %+v", cl)
	ds := inv.Datastores[0]
	t.Logf("datastore: %+v", ds)
	if ds.CapacityBytes == 0 {
		t.Errorf("datastore %s has no capacity", ds.Name)
	}
	t.Logf("network: %+v", inv.Networks[0])
}

// markTemplate powers off one VM and converts it to a template, so the split
// between ListVMs and ListTemplates is exercised rather than assumed.
func markTemplate(t *testing.T, c *vsphere.Client, name string) {
	t.Helper()
	ctx := context.Background()
	finder := find.NewFinder(c.VIM(), false)
	dc, err := finder.DefaultDatacenter(ctx)
	if err != nil {
		t.Fatalf("find default datacenter: %v", err)
	}
	finder.SetDatacenter(dc)
	vm, err := finder.VirtualMachine(ctx, name)
	if err != nil {
		t.Fatalf("find vm %s: %v", name, err)
	}
	task, err := vm.PowerOff(ctx)
	if err != nil {
		t.Fatalf("power off %s: %v", name, err)
	}
	if err := task.Wait(ctx); err != nil {
		t.Fatalf("power off %s: %v", name, err)
	}
	if err := vm.MarkAsTemplate(ctx); err != nil {
		t.Fatalf("mark %s as template: %v", name, err)
	}
}

func TestListTemplates(t *testing.T) {
	c := newSimulator(t, nil)
	ctx := context.Background()

	before, err := c.ListVMs(ctx)
	if err != nil {
		t.Fatalf("ListVMs: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("simulator produced no virtual machines")
	}
	markTemplate(t, c, before[0].Name)

	vms, err := c.ListVMs(ctx)
	if err != nil {
		t.Fatalf("ListVMs: %v", err)
	}
	for _, vm := range vms {
		if vm.IsTemplate {
			t.Errorf("ListVMs returned template %s", vm.Name)
		}
	}
	tmpl, err := c.ListTemplates(ctx)
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	for _, vm := range tmpl {
		if !vm.IsTemplate {
			t.Errorf("ListTemplates returned non-template %s", vm.Name)
		}
	}
	if len(tmpl) != 1 {
		t.Fatalf("expected exactly 1 template, got %d", len(tmpl))
	}
	if tmpl[0].Name != before[0].Name {
		t.Errorf("template is %q, want %q", tmpl[0].Name, before[0].Name)
	}
	if len(vms) != len(before)-1 {
		t.Errorf("ListVMs returned %d vms, want %d", len(vms), len(before)-1)
	}
}
