package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/apiresource"
	"github.com/easonliuuuuu/vsfleet/internal/query"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// getOptions are the flags "get" passes to every kind it lists.
type getOptions struct {
	where []string
	wide  bool
	// Scopes narrow a per-parent kind to one parent, by name: --host for host
	// subresources, --vm for snapshots, --switch for distributed port groups.
	scopes map[string]string
}

// kindListing is one kind's answer, ready to print.
type kindListing struct {
	kind    apiresource.Kind
	items   any
	headers []string
	rows    [][]string
	missing []string
}

// kindLister lists one kind for "get" and "cluster-info dump".
type kindLister interface {
	list(ctx context.Context, a *App, names []string, opt getOptions) (*kindListing, []contextFailure, error)
	// scope is the flag that narrows this kind to one parent, or "".
	scope() string
	// columns are the table headers, and whether --wide adds metadata.
	columns() (headers []string, wide bool)
}

// lister lists one kind whose objects are T. Kinds read from vCenter as a
// flat list (VMs, hosts) and kinds flattened out of a parent (a host's HBAs,
// a VM's snapshots) share it; the latter carry their parent in T.
type lister[T any] struct {
	// inventory enables --where for the kind; empty means it is not
	// supported, because the predicate engine knows inventory kinds only.
	inventory vsphere.Kind
	fetch     func(ctx context.Context, a *App, scope string) ([]T, []contextFailure, error)
	// names returns every name an object answers to: its name first, then
	// identifiers such as a VM's UUIDs.
	names   func(T) []string
	context func(T) string
	// metadata, when set, gives the kind TAGS and CUSTOM ATTRIBUTES columns
	// under --wide.
	metadata func(T) vsphere.Metadata
	headers  []string
	row      func(T) []string
	// json shapes the rows for -o json; nil writes them as they are.
	json      func([]T) any
	scopeFlag string
}

func (l lister[T]) scope() string { return l.scopeFlag }

func (l lister[T]) columns() ([]string, bool) { return l.headers, l.metadata != nil }

func (l lister[T]) list(ctx context.Context, a *App, names []string, opt getOptions) (*kindListing, []contextFailure, error) {
	var where query.Filter
	if len(opt.where) > 0 {
		if l.inventory == "" {
			return nil, nil, errors.New("--where applies to inventory kinds only")
		}
		var err error
		if where, err = query.Parse(opt.where, []vsphere.Kind{l.inventory}); err != nil {
			return nil, nil, fmt.Errorf("--where: %w", err)
		}
	}
	patterns, err := newNamePatterns(names)
	if err != nil {
		return nil, nil, err
	}
	items, failures, err := l.fetch(ctx, a, opt.scopes[l.scopeFlag])
	if err != nil {
		return nil, failures, err
	}
	reportMetadataWarnings(a, items)
	items = filterSlice(items, func(v T) bool {
		if !patterns.match(l.names(v)...) {
			return false
		}
		if where.Empty() {
			return true
		}
		subject, err := query.SubjectFromObject(l.inventory, v)
		return err == nil && where.Match(subject)
	})
	out := &kindListing{missing: patterns.missing()}
	if l.json != nil {
		out.items = l.json(items)
	} else {
		out.items = items
	}
	wide := opt.wide && l.metadata != nil
	multi := a.multiContext()
	out.headers = l.headers
	if wide {
		out.headers = append(slices.Clone(out.headers), "TAGS", "CUSTOM ATTRIBUTES")
	}
	if multi {
		out.headers = append([]string{"CONTEXT"}, out.headers...)
	}
	for _, v := range items {
		row := l.row(v)
		if wide {
			m := l.metadata(v)
			row = append(row, dash(metadataTags(m)), dash(metadataAttributes(m)))
		}
		if multi {
			row = append([]string{l.context(v)}, row...)
		}
		out.rows = append(out.rows, row)
	}
	return out, failures, nil
}

// listers holds every kind "get" can list, by canonical name.
var listers = map[string]kindLister{
	"vm": lister[vsphere.VM]{
		inventory: vsphere.KindVM,
		fetch:     gatherAll(func(ctx context.Context, c *vsphere.Client) ([]vsphere.VM, error) { return c.ListVMs(ctx) }),
		names:     vmNames,
		context:   vmContextOf,
		metadata:  func(v vsphere.VM) vsphere.Metadata { return v.Metadata },
		headers:   []string{"NAME", "STATE", "CPU", "RAM", "HOST", "IP"},
		row: func(v vsphere.VM) []string {
			return []string{v.Name, v.PowerState, i32toa(v.CPU), humanMB(v.MemoryMB), dash(v.Host), dash(v.IPAddress)}
		},
	},
	"template": lister[vsphere.VM]{
		inventory: vsphere.KindTemplate,
		fetch:     gatherAll(func(ctx context.Context, c *vsphere.Client) ([]vsphere.VM, error) { return c.ListTemplates(ctx) }),
		names:     vmNames,
		context:   vmContextOf,
		metadata:  func(v vsphere.VM) vsphere.Metadata { return v.Metadata },
		headers:   []string{"NAME", "OS", "CPU", "RAM", "DATASTORE", "FOLDER"},
		row: func(v vsphere.VM) []string {
			return []string{v.Name, dash(v.GuestOS), i32toa(v.CPU), humanMB(v.MemoryMB), dash(strings.Join(v.Datastores, ",")), dash(v.Folder)}
		},
	},
	"host": lister[vsphere.Host]{
		inventory: vsphere.KindHost,
		fetch:     gatherAll(func(ctx context.Context, c *vsphere.Client) ([]vsphere.Host, error) { return c.ListHosts(ctx) }),
		names:     func(h vsphere.Host) []string { return []string{h.Name} },
		context:   func(h vsphere.Host) string { return h.Context },
		metadata:  func(h vsphere.Host) vsphere.Metadata { return h.Metadata },
		headers:   []string{"NAME", "CLUSTER", "STATE", "CPU", "RAM", "VMS", "VERSION"},
		row: func(h vsphere.Host) []string {
			state := h.ConnectionState
			if h.InMaintenance {
				state += " (maintenance)"
			}
			return []string{h.Name, dash(h.Cluster), state, i32toa(h.CPUCores) + " cores", humanMB(h.MemoryMB), itoa(h.VMCount), dash(h.Version)}
		},
	},
	"cluster": lister[vsphere.Cluster]{
		inventory: vsphere.KindCluster,
		fetch:     gatherAll(func(ctx context.Context, c *vsphere.Client) ([]vsphere.Cluster, error) { return c.ListClusters(ctx) }),
		names:     func(c vsphere.Cluster) []string { return []string{c.Name} },
		context:   func(c vsphere.Cluster) string { return c.Context },
		metadata:  func(c vsphere.Cluster) vsphere.Metadata { return c.Metadata },
		headers:   []string{"NAME", "DATACENTER", "HOSTS", "CPU", "RAM", "DRS", "HA"},
		row: func(c vsphere.Cluster) []string {
			name := c.Name
			if c.Standalone {
				name += " (standalone)"
			}
			return []string{name, dash(c.Datacenter), itoa(c.Hosts), dash(mhz(c.TotalCPUMHz)), humanMB(c.TotalMemoryMB), onOff(c.DRSEnabled), onOff(c.HAEnabled)}
		},
	},
	"vapp": lister[vsphere.VApp]{
		inventory: vsphere.KindVApp,
		fetch:     gatherAll(func(ctx context.Context, c *vsphere.Client) ([]vsphere.VApp, error) { return c.ListVApps(ctx) }),
		names:     func(v vsphere.VApp) []string { return []string{v.Name} },
		context:   func(v vsphere.VApp) string { return v.Context },
		metadata:  func(v vsphere.VApp) vsphere.Metadata { return v.Metadata },
		headers:   []string{"NAME", "STATUS", "VMS", "CHILDREN", "DATACENTER", "PATH"},
		row: func(v vsphere.VApp) []string {
			children := fmt.Sprintf("%d vApp / %d pool", v.ChildVAppCount, v.ChildResourcePoolCount)
			return []string{v.Name, dash(v.Status), itoa(v.DirectVMCount), children, dash(v.Datacenter), dash(v.Path)}
		},
	},
	"datastore": lister[vsphere.Datastore]{
		inventory: vsphere.KindDatastore,
		fetch: gatherAll(func(ctx context.Context, c *vsphere.Client) ([]vsphere.Datastore, error) {
			return c.ListDatastores(ctx)
		}),
		names:    func(d vsphere.Datastore) []string { return []string{d.Name} },
		context:  func(d vsphere.Datastore) string { return d.Context },
		metadata: func(d vsphere.Datastore) vsphere.Metadata { return d.Metadata },
		headers:  []string{"NAME", "TYPE", "CAPACITY", "FREE", "USED", "DATACENTER"},
		row: func(d vsphere.Datastore) []string {
			used := "-"
			if d.CapacityBytes > 0 {
				used = fmt.Sprintf("%.0f%%", d.UsedPercent())
			}
			return []string{d.Name, dash(d.Type), humanBytes(d.CapacityBytes), humanBytes(d.FreeBytes), used, dash(d.Datacenter)}
		},
	},
	"network": lister[vsphere.Network]{
		inventory: vsphere.KindNetwork,
		fetch:     gatherAll(func(ctx context.Context, c *vsphere.Client) ([]vsphere.Network, error) { return c.ListNetworks(ctx) }),
		names:     func(n vsphere.Network) []string { return []string{n.Name} },
		context:   func(n vsphere.Network) string { return n.Context },
		metadata:  func(n vsphere.Network) vsphere.Metadata { return n.Metadata },
		headers:   []string{"NAME", "TYPE", "SWITCH", "VLAN", "DATACENTER", "ACCESSIBLE"},
		row: func(n vsphere.Network) []string {
			return []string{n.Name, n.Type, dash(n.Switch), dash(n.VLAN), dash(n.Datacenter), yesNo(n.Accessible)}
		},
	},
	"dvswitch": lister[vsphere.DVSwitch]{
		inventory: vsphere.KindDVSwitch,
		fetch:     gatherAll(func(ctx context.Context, c *vsphere.Client) ([]vsphere.DVSwitch, error) { return c.ListDVSwitches(ctx) }),
		names:     func(d vsphere.DVSwitch) []string { return []string{d.Name} },
		context:   func(d vsphere.DVSwitch) string { return d.Context },
		metadata:  func(d vsphere.DVSwitch) vsphere.Metadata { return d.Metadata },
		headers:   []string{"NAME", "VENDOR", "VERSION", "PORTS", "MAX PORTS", "HOSTS", "PORT GROUPS"},
		row: func(d vsphere.DVSwitch) []string {
			return []string{d.Name, dash(d.Vendor), dash(d.Version), i32toa(d.NumPorts), i32toa(d.MaxPorts), itoa(len(d.Hosts)), itoa(len(d.PortGroups))}
		},
	},
	"resourcepool": lister[vsphere.ResourcePool]{
		inventory: vsphere.KindResourcePool,
		fetch: gatherAll(func(ctx context.Context, c *vsphere.Client) ([]vsphere.ResourcePool, error) {
			return c.ListResourcePools(ctx)
		}),
		names:    func(p vsphere.ResourcePool) []string { return []string{p.Name} },
		context:  func(p vsphere.ResourcePool) string { return p.Context },
		metadata: func(p vsphere.ResourcePool) vsphere.Metadata { return p.Metadata },
		headers:  []string{"NAME", "PARENT", "ROOT", "VMS", "CPU SHARES", "CPU LEVEL", "MEM CONFIGURED", "MEM SHARES", "MEM LEVEL"},
		row: func(p vsphere.ResourcePool) []string {
			return []string{p.Name, dash(p.Parent), yesNo(p.Root), itoa(len(p.VMRefs)), i32toa(p.CPUShares), dash(p.CPULevel), humanMB(p.MemConfiguredMB), i32toa(p.MemShares), dash(p.MemLevel)}
		},
	},
	"dvportgroup": dvPortGroupLister(),
	"snapshot":    snapshotLister(),
	"hba": hostItemLister([]string{"DEVICE", "TYPE", "STATUS", "MODEL", "DRIVER", "PROTOCOL"},
		func(h vsphere.Host) []vsphere.HostHBA { return h.HBAs },
		func(hba vsphere.HostHBA) string { return hba.Device },
		func(hba vsphere.HostHBA) []string {
			return []string{hba.Device, dash(hba.Type), dash(hba.Status), dash(hba.Model), dash(hba.Driver), dash(hba.StorageProtocol)}
		}),
	"multipath": hostItemLister([]string{"LUN", "POLICY", "PATHS", "ACTIVE", "STANDBY", "DEAD", "DISABLED"},
		func(h vsphere.Host) []vsphere.HostMultipath { return h.Multipaths },
		func(mp vsphere.HostMultipath) string { return mp.LUN },
		func(mp vsphere.HostMultipath) []string {
			return []string{mp.LUN, dash(mp.Policy), itoa(mp.PathCount), itoa(mp.Active), itoa(mp.Standby), itoa(mp.Dead), itoa(mp.Disabled)}
		}),
	"pnic": hostItemLister([]string{"DEVICE", "MAC", "LINK SPEED", "DUPLEX", "SWITCH"},
		func(h vsphere.Host) []vsphere.HostNIC { return h.NICs },
		func(n vsphere.HostNIC) string { return n.Device },
		func(n vsphere.HostNIC) []string {
			return []string{n.Device, dash(n.MAC), linkSpeed(n.LinkSpeedMB), duplexLabel(n.Duplex), dash(n.Switch)}
		}),
	"vswitch": hostItemLister([]string{"NAME", "PORTS", "FREE", "MTU", "UPLINKS"},
		func(h vsphere.Host) []vsphere.HostVSwitch { return h.VSwitches },
		func(vs vsphere.HostVSwitch) string { return vs.Name },
		func(vs vsphere.HostVSwitch) []string {
			return []string{vs.Name, i32toa(vs.NumPorts), i32toa(vs.FreePorts), i32toa(vs.MTU), dash(strings.Join(vs.Uplinks, ","))}
		}),
	"portgroup": hostItemLister([]string{"NAME", "SWITCH", "VLAN"},
		func(h vsphere.Host) []vsphere.HostPortGroup { return h.PortGroups },
		func(pg vsphere.HostPortGroup) string { return pg.Name },
		func(pg vsphere.HostPortGroup) []string {
			return []string{pg.Name, dash(pg.Switch), i32toa(pg.VLAN)}
		}),
	"vmkernel": hostItemLister([]string{"DEVICE", "PORT GROUP", "IP", "MAC", "MTU"},
		func(h vsphere.Host) []vsphere.HostVMKernel { return h.VMKs },
		func(vmk vsphere.HostVMKernel) string { return vmk.Device },
		func(vmk vsphere.HostVMKernel) []string {
			return []string{vmk.Device, dash(vmk.PortGroup), dash(vmk.Address()), dash(vmk.MAC), i32toa(vmk.MTU)}
		}),
}

// gatherAll adapts a per-vCenter listing to a lister's fetch, for kinds that
// have no parent to narrow to.
func gatherAll[T any](fn func(context.Context, *vsphere.Client) ([]T, error)) func(context.Context, *App, string) ([]T, []contextFailure, error) {
	return func(ctx context.Context, a *App, _ string) ([]T, []contextFailure, error) {
		return gather(ctx, a, fn)
	}
}

func vmNames(v vsphere.VM) []string { return []string{v.Name, v.InstanceUUID, v.BIOSUUID} }

// hostItem is one piece of per-host evidence with the host it lives on.
type hostItem[T any] struct {
	host vsphere.Host
	item T
}

// hostItemLister flattens one piece of per-host evidence, such as Host.HBAs,
// across every host, optionally narrowed to one host with --host. Every row
// carries its host's context and name, so JSON output identifies where each
// device lives even when several hosts or vCenters are listed at once.
func hostItemLister[T any](headers []string, extract func(vsphere.Host) []T, name func(T) string, row func(T) []string) kindLister {
	return lister[hostItem[T]]{
		scopeFlag: "host",
		fetch: func(ctx context.Context, a *App, hostName string) ([]hostItem[T], []contextFailure, error) {
			hosts, failures, err := gather(ctx, a, func(ctx context.Context, c *vsphere.Client) ([]vsphere.Host, error) {
				return c.ListHostsWithConfig(ctx)
			})
			if err != nil {
				return nil, failures, err
			}
			if hostName != "" {
				host, err := exactlyOne(a, "host", hostName, filterByName(hosts, hostName, func(h vsphere.Host) string { return h.Name }),
					func(h vsphere.Host) string { return h.Context },
					func(h vsphere.Host) string { return h.Path },
					func(h vsphere.Host) string { return h.Name })
				if err != nil {
					return nil, failures, err
				}
				hosts = []vsphere.Host{host}
			}
			var out []hostItem[T]
			for _, h := range hosts {
				for _, item := range extract(h) {
					out = append(out, hostItem[T]{host: h, item: item})
				}
			}
			return out, failures, nil
		},
		names:   func(v hostItem[T]) []string { return []string{name(v.item)} },
		context: func(v hostItem[T]) string { return v.host.Context },
		headers: append([]string{"HOST"}, headers...),
		row:     func(v hostItem[T]) []string { return append([]string{v.host.Name}, row(v.item)...) },
		json: func(items []hostItem[T]) any {
			type wrapped struct {
				Context string `json:"context"`
				Host    string `json:"host"`
				Detail  T      `json:"detail"`
			}
			rows := make([]wrapped, 0, len(items))
			for _, v := range items {
				rows = append(rows, wrapped{Context: v.host.Context, Host: v.host.Name, Detail: v.item})
			}
			return rows
		},
	}
}

type vmSnapshot struct {
	vm   vsphere.VM
	snap vsphere.VMSnapshot
}

func snapshotLister() kindLister {
	return lister[vmSnapshot]{
		scopeFlag: "vm",
		fetch: func(ctx context.Context, a *App, vmName string) ([]vmSnapshot, []contextFailure, error) {
			vms, failures, err := gather(ctx, a, func(ctx context.Context, c *vsphere.Client) ([]vsphere.VM, error) {
				return c.ListVMs(ctx)
			})
			if err != nil {
				return nil, failures, err
			}
			if vmName != "" {
				vm, err := resolveVM(a, "vm", vmName, vms)
				if err != nil {
					return nil, failures, err
				}
				vms = []vsphere.VM{vm}
			}
			var out []vmSnapshot
			for _, v := range vms {
				for _, s := range v.Snapshots {
					out = append(out, vmSnapshot{vm: v, snap: s})
				}
			}
			return out, failures, nil
		},
		names:   func(v vmSnapshot) []string { return []string{v.snap.Name} },
		context: func(v vmSnapshot) string { return v.vm.Context },
		headers: []string{"VM", "NAME", "CREATED", "POWER STATE", "QUIESCED", "CURRENT"},
		row: func(v vmSnapshot) []string {
			s := v.snap
			return []string{v.vm.Name, s.Name, formatSnapshotTime(s.CreateTime), dash(s.PowerState), yesNo(s.Quiesced), yesNo(s.Current)}
		},
		json: func(items []vmSnapshot) any {
			type wrapped struct {
				Context string `json:"context"`
				VM      string `json:"vm"`
				vsphere.VMSnapshot
			}
			rows := make([]wrapped, 0, len(items))
			for _, v := range items {
				rows = append(rows, wrapped{Context: v.vm.Context, VM: v.vm.Name, VMSnapshot: v.snap})
			}
			return rows
		},
	}
}

type switchPortGroup struct {
	sw vsphere.DVSwitch
	pg vsphere.DVPortGroup
}

func dvPortGroupLister() kindLister {
	return lister[switchPortGroup]{
		scopeFlag: "switch",
		fetch: func(ctx context.Context, a *App, switchName string) ([]switchPortGroup, []contextFailure, error) {
			switches, failures, err := gather(ctx, a, func(ctx context.Context, c *vsphere.Client) ([]vsphere.DVSwitch, error) {
				return c.ListDVSwitches(ctx)
			})
			if err != nil {
				return nil, failures, err
			}
			if switchName != "" {
				sw, err := exactlyOne(a, "dvswitch", switchName, filterByName(switches, switchName, func(d vsphere.DVSwitch) string { return d.Name }),
					func(d vsphere.DVSwitch) string { return d.Context },
					func(d vsphere.DVSwitch) string { return d.Path },
					func(d vsphere.DVSwitch) string { return d.Name })
				if err != nil {
					return nil, failures, err
				}
				switches = []vsphere.DVSwitch{sw}
			}
			var out []switchPortGroup
			for _, d := range switches {
				for _, pg := range d.PortGroups {
					out = append(out, switchPortGroup{sw: d, pg: pg})
				}
			}
			return out, failures, nil
		},
		names:   func(v switchPortGroup) []string { return []string{v.pg.Name} },
		context: func(v switchPortGroup) string { return v.sw.Context },
		headers: []string{"SWITCH", "NAME", "TYPE", "VLAN", "PORTS", "UPLINK"},
		row: func(v switchPortGroup) []string {
			pg := v.pg
			return []string{v.sw.Name, pg.Name, dash(pg.Type), dash(pg.VLAN), i32toa(pg.NumPorts), yesNo(pg.Uplink)}
		},
		json: func(items []switchPortGroup) any {
			type wrapped struct {
				Context string `json:"context"`
				Switch  string `json:"switch"`
				vsphere.DVPortGroup
			}
			rows := make([]wrapped, 0, len(items))
			for _, v := range items {
				rows = append(rows, wrapped{Context: v.sw.Context, Switch: v.sw.Name, DVPortGroup: v.pg})
			}
			return rows
		},
	}
}

// scopeFlags are the parent-narrowing flags and the kinds each applies to.
var scopeFlags = []struct{ flag, usage string }{
	{"host", "only include this host's evidence, by name (host subresource kinds)"},
	{"vm", "only include this VM's snapshots, by name or UUID"},
	{"switch", "only include port groups on this distributed switch, by name"},
}

func newGetCommand(a *App) *cobra.Command {
	opt := getOptions{scopes: map[string]string{}}
	scopes := make([]string, len(scopeFlags))
	cmd := &cobra.Command{
		Use:   "get KIND[,KIND...] [NAME...] | KIND/NAME...",
		Short: "List objects of one or more kinds, read live from vCenter",
		Long: strings.TrimSpace(`
List objects the way "kubectl get" does. KIND is any name or short name
"vsfleet api-resources" shows, and several kinds can be listed at once,
separated by commas. NAME narrows the listing: an exact name (ignoring case),
a VM's instance or BIOS UUID, or a shell glob such as 'web*'. Quote a glob so
the shell does not expand it. An exact name that matches nothing is an error;
a glob that matches nothing is an empty answer.

-n picks a context and -A spans every context; a CONTEXT column appears
whenever more than one is in play. A context that cannot be reached is
reported after the rows that could be read, never instead of them.

With one kind, -o json writes a JSON array. With several, it writes one
object holding an array per kind, keyed by the kind's plural.`),
		Example: `  # Every VM on the current context
  vsfleet get vms

  # Every VM on every context
  vsfleet get vms -A

  # VMs whose name starts with web, and hosts too
  vsfleet get vms,hosts 'web*'

  # One VM and one host, by KIND/NAME
  vsfleet get vm/web-01 host/esxi-07

  # HBAs on one host, as JSON
  vsfleet get hbas --host esxi-07 -o json

  # Production VMs with eight or more vCPUs, with their tags
  vsfleet get vms --where 'tag=Production' --where 'cpu>=8' --wide`,
		// parseResourceArgs reports a missing KIND with where to find one.
		Args: cobra.ArbitraryArgs,
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return gettableKindNames(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			reqs, err := parseResourceArgs(args)
			if err != nil {
				return err
			}
			for i, s := range scopeFlags {
				if scopes[i] != "" {
					opt.scopes[s.flag] = scopes[i]
				}
			}
			return runGet(cmd.Context(), a, reqs, opt)
		},
	}
	cmd.Flags().StringArrayVar(&opt.where, "where", nil, "structured predicate (repeatable; predicates are ANDed)")
	cmd.Flags().BoolVar(&opt.wide, "wide", false, "include tags and custom attributes")
	for i, s := range scopeFlags {
		cmd.Flags().StringVar(&scopes[i], s.flag, "", s.usage)
	}
	return cmd
}

// gettableKindNames are the plurals "get" lists, for completion.
func gettableKindNames() []string {
	var out []string
	for _, k := range apiresource.All() {
		if _, ok := listers[k.Name]; ok {
			out = append(out, k.Plural)
		}
	}
	return out
}

func listerFor(kind apiresource.Kind) (kindLister, error) {
	l, ok := listers[kind.Name]
	if !ok {
		if kind.Name == "event" {
			return nil, errors.New(`events are not listed with get; use "vsfleet events --for vm/NAME"`)
		}
		return nil, fmt.Errorf("%s cannot be listed with get", kind.Plural)
	}
	return l, nil
}

func runGet(ctx context.Context, a *App, reqs []resourceRequest, opt getOptions) error {
	type pending struct {
		req    resourceRequest
		lister kindLister
	}
	work := make([]pending, 0, len(reqs))
	for _, r := range reqs {
		l, err := listerFor(r.kind)
		if err != nil {
			return err
		}
		work = append(work, pending{r, l})
	}
	// A scope flag that no requested kind takes is a mistake, not a no-op:
	// "get vms --host esxi-07" would otherwise list every VM.
	for flag := range opt.scopes {
		if !slices.ContainsFunc(work, func(p pending) bool { return p.lister.scope() == flag }) {
			return fmt.Errorf("--%s applies to %s only", flag, strings.Join(kindsWithScope(flag), ", "))
		}
	}

	var (
		listings []*kindListing
		failures contextFailures
	)
	for _, p := range work {
		l, failed, err := p.lister.list(ctx, a, p.req.names, opt)
		failures.add(failed)
		if err != nil {
			reportFailures(a, failures.list)
			return err
		}
		l.kind = p.req.kind
		listings = append(listings, l)
	}
	defer reportFailures(a, failures.list)

	if a.json() {
		if err := writeListingsJSON(a, listings); err != nil {
			return err
		}
	} else {
		printListings(a, listings)
	}
	return missingError(listings)
}

func writeListingsJSON(a *App, listings []*kindListing) error {
	if len(listings) == 1 {
		return writeJSON(a.out(), listings[0].items)
	}
	byKind := make(map[string]any, len(listings))
	for _, l := range listings {
		byKind[l.kind.Plural] = l.items
	}
	return writeJSON(a.out(), byKind)
}

// printListings writes one table per kind, a blank line apart, the way
// "kubectl get pods,services" does. When nothing matched at all, the note
// goes to stderr so a script reading stdout sees an empty answer.
func printListings(a *App, listings []*kindListing) {
	printed := 0
	for _, l := range listings {
		if len(l.rows) == 0 {
			continue
		}
		if printed > 0 {
			fmt.Fprintln(a.out())
		}
		t := newTable(a.out(), l.headers...)
		for _, row := range l.rows {
			t.row(row...)
		}
		t.flush()
		printed++
	}
	if printed == 0 {
		fmt.Fprintln(a.errOut(), "No resources found.")
	}
}

func missingError(listings []*kindListing) error {
	var errs []error
	for _, l := range listings {
		for _, name := range l.missing {
			errs = append(errs, &noMatchError{kind: l.kind.Name, query: name})
		}
	}
	return errors.Join(errs...)
}

func kindsWithScope(flag string) []string {
	var out []string
	for _, k := range apiresource.All() {
		if l, ok := listers[k.Name]; ok && l.scope() == flag {
			out = append(out, k.Plural)
		}
	}
	return out
}

// contextFailures collects the contexts that could not answer across several
// listings. Every listing connects to the same contexts, so one unreachable
// vCenter fails each of them; it is reported once.
type contextFailures struct {
	list []contextFailure
}

func (c *contextFailures) add(failed []contextFailure) {
	for _, f := range failed {
		if !slices.ContainsFunc(c.list, func(g contextFailure) bool { return g.Context == f.Context }) {
			c.list = append(c.list, f)
		}
	}
}
func linkSpeed(mb *int32) string {
	if mb == nil {
		return "-"
	}
	return itoa(int(*mb)) + " Mb"
}

func duplexLabel(full *bool) string {
	if full == nil {
		return "-"
	}
	if *full {
		return "full"
	}
	return "half"
}
