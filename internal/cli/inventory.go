package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/easonliuuuuu/vc-tui/internal/session"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

// contextFailure records that one vCenter could not answer. Listing commands
// print the rows they did get and report these separately.
type contextFailure struct {
	Context string
	Err     error
}

// gather queries every selected context concurrently and merges the results.
// A failure is attached to its context and never cancels the others.
func gather[T any](ctx context.Context, a *App, fn func(context.Context, *vsphere.Client) ([]T, error)) ([]T, []contextFailure, error) {
	contexts, err := a.Contexts()
	if err != nil {
		return nil, nil, err
	}
	mgr := a.Sessions()

	type result struct {
		items []T
		err   error
	}
	results := make([]result, len(contexts))

	var g errgroup.Group
	g.SetLimit(session.DefaultConcurrency)
	for i, cc := range contexts {
		g.Go(func() error {
			s, err := mgr.Connect(ctx, cc)
			if err != nil {
				results[i] = result{err: err}
				return nil
			}
			items, err := fn(ctx, s.Client())
			results[i] = result{items: items, err: err}
			return nil
		})
	}
	_ = g.Wait()

	var (
		all      []T
		failures []contextFailure
	)
	for i, r := range results {
		if r.err != nil {
			failures = append(failures, contextFailure{Context: contexts[i].Name, Err: r.err})
			continue
		}
		all = append(all, r.items...)
	}
	if len(all) == 0 && len(failures) == len(contexts) && len(failures) > 0 {
		return nil, failures, fmt.Errorf("no context could be queried")
	}
	return all, failures, nil
}

// reportFailures prints the contexts that could not answer, after the rows
// that did. Partial results with a visible gap beat an empty screen.
func reportFailures(a *App, failures []contextFailure) {
	if len(failures) == 0 {
		return
	}
	fmt.Fprintln(a.errOut())
	for _, f := range failures {
		fmt.Fprintf(a.errOut(), "%s %s: %v\n", glyphFail, f.Context, f.Err)
	}
}

// multiContext reports whether output should carry a CONTEXT column.
func (a *App) multiContext() bool {
	if a.AllContexts || len(a.ContextNames) > 1 {
		return true
	}
	cfg, err := a.Config()
	if err != nil {
		return false
	}
	return len(a.ContextNames) == 0 && cfg.CurrentContext == "" && len(cfg.Contexts) > 1
}

// listFlags are shared by every inventory listing command.
type listFlags struct {
	filter string
}

func (f *listFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.filter, "filter", "f", "", "only show objects whose name contains this text")
}

func (f *listFlags) matches(name string) bool {
	if f.filter == "" {
		return true
	}
	return strings.Contains(strings.ToLower(name), strings.ToLower(f.filter))
}

// newInventoryCommands builds one command group per resource kind. They all
// share the same shape so that "vctui <kind> list" is predictable.
func newInventoryCommands(a *App) []*cobra.Command {
	return []*cobra.Command{
		group("vm", []string{"vms", "virtualmachine"}, "Virtual machines", newVMListCommand(a)),
		group("template", []string{"templates", "tpl"}, "VM templates", newTemplateListCommand(a)),
		group("host", []string{"hosts", "esxi"}, "ESXi hosts", newHostListCommand(a)),
		group("cluster", []string{"clusters"}, "Compute clusters", newClusterListCommand(a)),
		group("datastore", []string{"datastores", "ds"}, "Datastores", newDatastoreListCommand(a)),
		group("network", []string{"networks", "portgroup"}, "Networks and port groups", newNetworkListCommand(a)),
	}
}

func group(name string, aliases []string, short string, sub *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{Use: name, Aliases: aliases, Short: short}
	cmd.AddCommand(sub)
	return cmd
}

func listCommand(a *App, short string, run func(*cobra.Command, *listFlags) error) *cobra.Command {
	var f listFlags
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   short,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd, &f)
		},
	}
	f.register(cmd)
	return cmd
}

func newVMListCommand(a *App) *cobra.Command {
	return listCommand(a, "List virtual machines", func(cmd *cobra.Command, f *listFlags) error {
		vms, failures, err := gather(cmd.Context(), a, func(ctx context.Context, c *vsphere.Client) ([]vsphere.VM, error) {
			return c.ListVMs(ctx)
		})
		if err != nil {
			reportFailures(a, failures)
			return err
		}
		vms = filterSlice(vms, func(v vsphere.VM) bool { return f.matches(v.Name) })
		if a.json() {
			defer reportFailures(a, failures)
			return writeJSON(a.out(), vms)
		}
		headers := []string{"NAME", "STATE", "CPU", "RAM", "HOST", "IP"}
		multi := a.multiContext()
		if multi {
			headers = append([]string{"CONTEXT"}, headers...)
		}
		t := newTable(a.out(), headers...)
		for _, v := range vms {
			row := []string{v.Name, v.PowerState, i32toa(v.CPU), humanMB(v.MemoryMB), dash(v.Host), dash(v.IPAddress)}
			if multi {
				row = append([]string{v.Context}, row...)
			}
			t.row(row...)
		}
		t.flush()
		reportFailures(a, failures)
		return nil
	})
}

func newTemplateListCommand(a *App) *cobra.Command {
	return listCommand(a, "List VM templates", func(cmd *cobra.Command, f *listFlags) error {
		tmpl, failures, err := gather(cmd.Context(), a, func(ctx context.Context, c *vsphere.Client) ([]vsphere.VM, error) {
			return c.ListTemplates(ctx)
		})
		if err != nil {
			reportFailures(a, failures)
			return err
		}
		tmpl = filterSlice(tmpl, func(v vsphere.VM) bool { return f.matches(v.Name) })
		if a.json() {
			defer reportFailures(a, failures)
			return writeJSON(a.out(), tmpl)
		}
		headers := []string{"NAME", "OS", "CPU", "RAM", "DATASTORE", "FOLDER"}
		multi := a.multiContext()
		if multi {
			headers = append([]string{"CONTEXT"}, headers...)
		}
		t := newTable(a.out(), headers...)
		for _, v := range tmpl {
			row := []string{v.Name, dash(v.GuestOS), i32toa(v.CPU), humanMB(v.MemoryMB), dash(strings.Join(v.Datastores, ",")), dash(v.Folder)}
			if multi {
				row = append([]string{v.Context}, row...)
			}
			t.row(row...)
		}
		t.flush()
		reportFailures(a, failures)
		return nil
	})
}

func newHostListCommand(a *App) *cobra.Command {
	return listCommand(a, "List ESXi hosts", func(cmd *cobra.Command, f *listFlags) error {
		hosts, failures, err := gather(cmd.Context(), a, func(ctx context.Context, c *vsphere.Client) ([]vsphere.Host, error) {
			return c.ListHosts(ctx)
		})
		if err != nil {
			reportFailures(a, failures)
			return err
		}
		hosts = filterSlice(hosts, func(h vsphere.Host) bool { return f.matches(h.Name) })
		if a.json() {
			defer reportFailures(a, failures)
			return writeJSON(a.out(), hosts)
		}
		headers := []string{"NAME", "CLUSTER", "STATE", "CPU", "RAM", "VMS", "VERSION"}
		multi := a.multiContext()
		if multi {
			headers = append([]string{"CONTEXT"}, headers...)
		}
		t := newTable(a.out(), headers...)
		for _, h := range hosts {
			state := h.ConnectionState
			if h.InMaintenance {
				state += " (maintenance)"
			}
			row := []string{h.Name, dash(h.Cluster), state, i32toa(h.CPUCores) + " cores", humanMB(h.MemoryMB), itoa(h.VMCount), dash(h.Version)}
			if multi {
				row = append([]string{h.Context}, row...)
			}
			t.row(row...)
		}
		t.flush()
		reportFailures(a, failures)
		return nil
	})
}

func newClusterListCommand(a *App) *cobra.Command {
	return listCommand(a, "List compute clusters", func(cmd *cobra.Command, f *listFlags) error {
		clusters, failures, err := gather(cmd.Context(), a, func(ctx context.Context, c *vsphere.Client) ([]vsphere.Cluster, error) {
			return c.ListClusters(ctx)
		})
		if err != nil {
			reportFailures(a, failures)
			return err
		}
		clusters = filterSlice(clusters, func(c vsphere.Cluster) bool { return f.matches(c.Name) })
		if a.json() {
			defer reportFailures(a, failures)
			return writeJSON(a.out(), clusters)
		}
		headers := []string{"NAME", "DATACENTER", "HOSTS", "CPU", "RAM", "DRS", "HA"}
		multi := a.multiContext()
		if multi {
			headers = append([]string{"CONTEXT"}, headers...)
		}
		t := newTable(a.out(), headers...)
		for _, c := range clusters {
			name := c.Name
			if c.Standalone {
				name += " (standalone)"
			}
			row := []string{name, dash(c.Datacenter), itoa(c.Hosts), dash(mhz(c.TotalCPUMHz)), humanMB(c.TotalMemoryMB), onOff(c.DRSEnabled), onOff(c.HAEnabled)}
			if multi {
				row = append([]string{c.Context}, row...)
			}
			t.row(row...)
		}
		t.flush()
		reportFailures(a, failures)
		return nil
	})
}

func newDatastoreListCommand(a *App) *cobra.Command {
	return listCommand(a, "List datastores", func(cmd *cobra.Command, f *listFlags) error {
		stores, failures, err := gather(cmd.Context(), a, func(ctx context.Context, c *vsphere.Client) ([]vsphere.Datastore, error) {
			return c.ListDatastores(ctx)
		})
		if err != nil {
			reportFailures(a, failures)
			return err
		}
		stores = filterSlice(stores, func(d vsphere.Datastore) bool { return f.matches(d.Name) })
		if a.json() {
			defer reportFailures(a, failures)
			return writeJSON(a.out(), stores)
		}
		headers := []string{"NAME", "TYPE", "CAPACITY", "FREE", "USED", "DATACENTER"}
		multi := a.multiContext()
		if multi {
			headers = append([]string{"CONTEXT"}, headers...)
		}
		t := newTable(a.out(), headers...)
		for _, d := range stores {
			used := "-"
			if d.CapacityBytes > 0 {
				used = fmt.Sprintf("%.0f%%", d.UsedPercent())
			}
			row := []string{d.Name, dash(d.Type), humanBytes(d.CapacityBytes), humanBytes(d.FreeBytes), used, dash(d.Datacenter)}
			if multi {
				row = append([]string{d.Context}, row...)
			}
			t.row(row...)
		}
		t.flush()
		reportFailures(a, failures)
		return nil
	})
}

func newNetworkListCommand(a *App) *cobra.Command {
	return listCommand(a, "List networks and port groups", func(cmd *cobra.Command, f *listFlags) error {
		nets, failures, err := gather(cmd.Context(), a, func(ctx context.Context, c *vsphere.Client) ([]vsphere.Network, error) {
			return c.ListNetworks(ctx)
		})
		if err != nil {
			reportFailures(a, failures)
			return err
		}
		nets = filterSlice(nets, func(n vsphere.Network) bool { return f.matches(n.Name) })
		if a.json() {
			defer reportFailures(a, failures)
			return writeJSON(a.out(), nets)
		}
		headers := []string{"NAME", "TYPE", "DATACENTER", "ACCESSIBLE"}
		multi := a.multiContext()
		if multi {
			headers = append([]string{"CONTEXT"}, headers...)
		}
		t := newTable(a.out(), headers...)
		for _, n := range nets {
			row := []string{n.Name, n.Type, dash(n.Datacenter), yesNo(n.Accessible)}
			if multi {
				row = append([]string{n.Context}, row...)
			}
			t.row(row...)
		}
		t.flush()
		reportFailures(a, failures)
		return nil
	})
}

func filterSlice[T any](in []T, keep func(T) bool) []T {
	out := in[:0]
	for _, v := range in {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}

func mhz(v int64) string {
	switch {
	case v <= 0:
		return ""
	case v >= 1000:
		return fmt.Sprintf("%.1f GHz", float64(v)/1000)
	default:
		return fmt.Sprintf("%d MHz", v)
	}
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
