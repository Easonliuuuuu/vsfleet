package vsphere

import (
	"context"
	"sort"
	"strings"

	"github.com/vmware/govmomi/vim25/mo"
)

var vmProps = []string{
	"name",
	"parent",
	"config.template",
	"config.guestFullName",
	"config.annotation",
	"config.hardware.numCPU",
	"config.hardware.memoryMB",
	"runtime.powerState",
	"runtime.host",
	"guest.ipAddress",
	"guest.guestState",
	"guest.toolsRunningStatus",
	"summary.storage.committed",
	"datastore",
}

// ListVMs returns the virtual machines in a vCenter, excluding templates.
func (c *Client) ListVMs(ctx context.Context) ([]VM, error) {
	all, err := c.listAllVMs(ctx)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, vm := range all {
		if !vm.IsTemplate {
			out = append(out, vm)
		}
	}
	return out, nil
}

func (c *Client) listAllVMs(ctx context.Context) ([]VM, error) {
	idx, err := newIndex(ctx, c)
	if err != nil {
		return nil, err
	}
	return c.listVMs(ctx, idx)
}

func (c *Client) listVMs(ctx context.Context, idx *index) ([]VM, error) {
	var raw []mo.VirtualMachine
	if err := retrieve(ctx, c, []string{"VirtualMachine"}, []string{"VirtualMachine"}, vmProps, &raw); err != nil {
		return nil, err
	}
	out := make([]VM, 0, len(raw))
	for i := range raw {
		out = append(out, newVM(c, idx, &raw[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func newVM(c *Client, idx *index, m *mo.VirtualMachine) VM {
	loc := idx.locate(c, m.Self, m.Name)
	vm := VM{
		Location:   loc,
		ID:         m.Self.Value,
		Name:       m.Name,
		PowerState: string(m.Runtime.PowerState),
		Host:       idx.name(m.Runtime.Host),
		Cluster:    idx.clusterOf(m.Runtime.Host),
		Folder:     idx.folderPath(m.Parent, loc.Datacenter),
		Datastores: idx.names(m.Datastore),
	}
	if cfg := m.Config; cfg != nil {
		vm.IsTemplate = cfg.Template
		vm.GuestOS = cfg.GuestFullName
		vm.Annotation = strings.TrimSpace(cfg.Annotation)
		vm.CPU = cfg.Hardware.NumCPU
		vm.MemoryMB = int64(cfg.Hardware.MemoryMB)
	}
	if g := m.Guest; g != nil {
		vm.IPAddress = g.IpAddress
		vm.GuestState = g.GuestState
		vm.ToolsState = g.ToolsRunningStatus
		if vm.GuestOS == "" {
			vm.GuestOS = g.GuestFullName
		}
	}
	if s := m.Summary.Storage; s != nil {
		vm.StorageGB = float64(s.Committed) / (1 << 30)
	}
	return vm
}
