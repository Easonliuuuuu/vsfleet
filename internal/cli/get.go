package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/signal"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/apiresource"
	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/jsonpath"
	"github.com/easonliuuuuu/vsfleet/internal/query"
	"github.com/easonliuuuuu/vsfleet/internal/tui"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// getOptions are the flags "get" passes to every kind it lists.
type getOptions struct {
	// fields is the raw --field-selector, parsed per kind because each kind
	// has its own fields.
	fields string
	labels query.LabelSelector
	wide   bool
	sortBy *jsonpath.Template
	// Scopes narrow a per-parent kind to one parent, by name: --host for host
	// subresources, --vm for snapshots, --switch for distributed port groups.
	scopes map[string]string
}

// kindListing is one kind's answer, ready to print.
type kindListing struct {
	kind    apiresource.Kind
	headers []string
	// rows, raw, objects and names are aligned: one entry per object, as a
	// table row, as its JSON, decoded for jsonpath, and by name.
	rows    [][]string
	raw     []json.RawMessage
	objects []any
	names   []string
	missing []string
	// all is set when the kind came from "all".
	all bool
	// warnings are the metadata sources these objects were not fully read
	// from.
	warnings []string
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
	// inventory enables --field-selector and -l for the kind; empty means it is not
	// supported, because the predicate engine knows inventory kinds only.
	inventory vsphere.Kind
	fetch     func(ctx context.Context, a *App, scope string) ([]T, []contextFailure, error)
	// names returns every name an object answers to: its name first, then
	// identifiers such as a VM's UUIDs.
	names   func(T) []string
	context func(T) string
	// metadata, when set, gives the kind TAGS and CUSTOM ATTRIBUTES columns
	// under -o wide.
	metadata func(T) vsphere.Metadata
	headers  []string
	row      func(T) []string
	// json shapes the rows for -o json; nil writes them as they are.
	json      func([]T) any
	scopeFlag string
	// history marks a kind the history database keeps as an object of its
	// own. It is read from there whatever -n and --run say, and carries its
	// own context column, if any.
	history bool
}

func (l lister[T]) scope() string { return l.scopeFlag }

func (l lister[T]) columns() ([]string, bool) { return l.headers, l.metadata != nil }

func (l lister[T]) list(ctx context.Context, a *App, names []string, opt getOptions) (*kindListing, []contextFailure, error) {
	var fields query.Filter
	if opt.fields != "" || !opt.labels.Empty() {
		if l.inventory == "" {
			return nil, nil, errors.New("--field-selector and -l apply to inventory kinds only")
		}
	}
	if opt.fields != "" {
		var err error
		if fields, err = query.ParseFieldSelector(opt.fields, []vsphere.Kind{l.inventory}); err != nil {
			return nil, nil, fmt.Errorf("--field-selector: %w", err)
		}
	}
	if l.history && a.capture != nil {
		return nil, nil, errors.New("captures and performance collections are listed from the capture database; --run does not apply to them")
	}
	patterns, err := newNamePatterns(names)
	if err != nil {
		return nil, nil, err
	}
	items, failures, err := l.fetch(ctx, a, opt.scopes[l.scopeFlag])
	if err != nil {
		return nil, failures, err
	}
	items = filterSlice(items, func(v T) bool {
		if !patterns.match(l.names(v)...) {
			return false
		}
		if fields.Empty() && opt.labels.Empty() {
			return true
		}
		subject, err := query.SubjectFromObject(l.inventory, v)
		return err == nil && fields.Match(subject) && opt.labels.Match(subject)
	})
	out := &kindListing{missing: patterns.missing(), warnings: metadataWarnings(items)}
	var shaped any = items
	if l.json != nil {
		shaped = l.json(items)
	}
	if err := decodeObjects(shaped, out); err != nil {
		return nil, failures, err
	}
	wide := opt.wide && l.metadata != nil
	multi := !l.history && a.multiContext()
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
		out.names = append(out.names, l.names(v)[0])
	}
	if opt.sortBy != nil {
		sortRows(out, opt.sortBy)
	}
	return out, failures, nil
}

// decodeObjects records each object as JSON, which keeps the domain type's
// field order for -o json and yaml, and decoded, for jsonpath and sorting.
func decodeObjects(shaped any, out *kindListing) error {
	data, err := json.Marshal(shaped)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &out.raw); err != nil {
		return err
	}
	if out.raw == nil {
		out.raw = []json.RawMessage{}
	}
	out.objects = make([]any, len(out.raw))
	for i, r := range out.raw {
		if err := json.Unmarshal(r, &out.objects[i]); err != nil {
			return err
		}
	}
	return nil
}

// listers holds every kind "get" can list, by canonical name.
var listers = map[string]kindLister{
	"vm": lister[vsphere.VM]{
		inventory: vsphere.KindVM,
		fetch:     readAll("vm", func(ctx context.Context, c *vsphere.Client) ([]vsphere.VM, error) { return c.ListVMs(ctx) }, storedVMs(false)),
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
		fetch:     readAll("vm", func(ctx context.Context, c *vsphere.Client) ([]vsphere.VM, error) { return c.ListTemplates(ctx) }, storedVMs(true)),
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
		fetch:     readAll("host", func(ctx context.Context, c *vsphere.Client) ([]vsphere.Host, error) { return c.ListHosts(ctx) }, storedObjects[vsphere.Host](vsphere.KindHost)),
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
		fetch:     readAll("cluster", func(ctx context.Context, c *vsphere.Client) ([]vsphere.Cluster, error) { return c.ListClusters(ctx) }, storedObjects[vsphere.Cluster](vsphere.KindCluster)),
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
		fetch:     readAll("vapp", func(ctx context.Context, c *vsphere.Client) ([]vsphere.VApp, error) { return c.ListVApps(ctx) }, storedObjects[vsphere.VApp](vsphere.KindVApp)),
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
		fetch: readAll("datastore", func(ctx context.Context, c *vsphere.Client) ([]vsphere.Datastore, error) {
			return c.ListDatastores(ctx)
		}, storedObjects[vsphere.Datastore](vsphere.KindDatastore)),
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
		fetch:     readAll("network", func(ctx context.Context, c *vsphere.Client) ([]vsphere.Network, error) { return c.ListNetworks(ctx) }, storedObjects[vsphere.Network](vsphere.KindNetwork)),
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
		fetch:     readAll("dvswitch", func(ctx context.Context, c *vsphere.Client) ([]vsphere.DVSwitch, error) { return c.ListDVSwitches(ctx) }, storedObjects[vsphere.DVSwitch](vsphere.KindDVSwitch)),
		names:     func(d vsphere.DVSwitch) []string { return []string{d.Name} },
		context:   func(d vsphere.DVSwitch) string { return d.Context },
		metadata:  func(d vsphere.DVSwitch) vsphere.Metadata { return d.Metadata },
		headers:   []string{"NAME", "VENDOR", "VERSION", "PORTS", "MAX PORTS", "HOSTS", "PORT GROUPS"},
		row: func(d vsphere.DVSwitch) []string {
			return []string{d.Name, dash(d.Vendor), dash(d.Version), i32toa(d.NumPorts), maxPorts(d.MaxPorts), itoa(len(d.Hosts)), itoa(len(d.PortGroups))}
		},
	},
	"resourcepool": lister[vsphere.ResourcePool]{
		inventory: vsphere.KindResourcePool,
		fetch: readAll("resourcepool", func(ctx context.Context, c *vsphere.Client) ([]vsphere.ResourcePool, error) {
			return c.ListResourcePools(ctx)
		}, storedObjects[vsphere.ResourcePool](vsphere.KindResourcePool)),
		names:    func(p vsphere.ResourcePool) []string { return []string{p.Name} },
		context:  func(p vsphere.ResourcePool) string { return p.Context },
		metadata: func(p vsphere.ResourcePool) vsphere.Metadata { return p.Metadata },
		headers:  []string{"NAME", "PARENT", "ROOT", "VMS", "CPU SHARES", "CPU LEVEL", "MEM CONFIGURED", "MEM SHARES", "MEM LEVEL"},
		row: func(p vsphere.ResourcePool) []string {
			return []string{p.Name, dash(p.Parent), yesNo(p.Root), itoa(len(p.VMRefs)), i32toa(p.CPUShares), dash(p.CPULevel), humanMB(p.MemConfiguredMB), i32toa(p.MemShares), dash(p.MemLevel)}
		},
	},
	"dvportgroup":    dvPortGroupLister(),
	"capture":        captureLister(),
	"perfcollection": perfCollectionLister(),
	"snapshot":       snapshotLister(),
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

// readAll adapts a live listing and its stored counterpart to a lister's
// fetch, for kinds that have no parent to narrow to.
func readAll[T any](kind string, live func(context.Context, *vsphere.Client) ([]T, error), stored func(*assessment.ExportData) ([]T, error)) func(context.Context, *App, string) ([]T, []contextFailure, error) {
	return func(ctx context.Context, a *App, _ string) ([]T, []contextFailure, error) {
		return read(ctx, a, kind, live, stored)
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
			hosts, failures, err := read(ctx, a, "host", func(ctx context.Context, c *vsphere.Client) ([]vsphere.Host, error) {
				return c.ListHostsWithConfig(ctx)
			}, storedObjects[vsphere.Host](vsphere.KindHost))
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
			vms, failures, err := read(ctx, a, "vm", func(ctx context.Context, c *vsphere.Client) ([]vsphere.VM, error) {
				return c.ListVMs(ctx)
			}, storedVMs(false))
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
			switches, failures, err := read(ctx, a, "dvswitch", func(ctx context.Context, c *vsphere.Client) ([]vsphere.DVSwitch, error) {
				return c.ListDVSwitches(ctx)
			}, storedObjects[vsphere.DVSwitch](vsphere.KindDVSwitch))
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

// getFlags are the flags "get" reads besides the scopes.
type getFlags struct {
	fieldSelector string
	labelSelector string
	sortBy        string
	noHeaders     bool
	watch         bool
	run           string
	// where and wide are the removed spellings, kept only to say where they
	// went.
	where []string
	wide  bool
}

func newGetCommand(a *App) *cobra.Command {
	var f getFlags
	scopes := make([]string, len(scopeFlags))
	cmd := &cobra.Command{
		Use:   "get KIND[,KIND...] [NAME...] | KIND/NAME...",
		Short: "List objects of one or more kinds, live or from a capture",
		Long: strings.TrimSpace(`
List objects the way "kubectl get" does. KIND is any name or short name
"vsfleet api-resources" shows, and several kinds can be listed at once,
separated by commas; "all" lists every inventory kind. NAME narrows the
listing: an exact name (ignoring case),
a VM's instance or BIOS UUID, or a shell glob such as 'web*'. Quote a glob so
the shell does not expand it. An exact name that matches nothing is an error;
a glob that matches nothing is an empty answer.

-l selects by vSphere tag, with the tag category as the label key and the tag
name as its value: env=prod, env!=prod, 'env in (prod,dev)', env, !env.
--field-selector selects by field, in the --where language, comma-separated:
'cpu>=8,power_state=poweredOn'. "vsfleet explain KIND" lists a kind's fields.
A VM whose tags could not be read matches no label selector, negative or not.

-o picks the output: table (default), wide (adds tags and custom attributes),
json, yaml, name (KIND/NAME, ready for "vsfleet describe"), csv (the table's
rows and columns, one kind at a time), jsonpath=TEMPLATE, or
custom-columns=NAME:EXPR,... --sort-by takes a JSONPath expression such as
.cpu. With one kind, json and yaml write an array; with several, an object
holding an array per kind, keyed by the kind's plural. jsonpath sees
{"kind": "List", "items": [...]}, or the object itself when one was named.

-n picks a context and -A spans every context; a CONTEXT column appears
whenever more than one is in play. A context that cannot be reached is
reported after the rows that could be read, never instead of them.

Objects are read live from vCenter. --run reads them from a stored capture
instead: an ID, a label, latest, previous, or a date (2026-03-31, or
2026-03-31T18:00) for the newest capture started by then. It reads the same
contexts a live read would, and never contacts vCenter; a live read never
falls back to stored data.

-w re-reads and re-renders on the interface's refresh interval (--refresh)
until interrupted.`),
		Example: `  # Every VM on the current context
  vsfleet get vms

  # Every VM on every context, with tags and custom attributes
  vsfleet get vms -A -o wide

  # VMs whose name starts with web, and hosts too
  vsfleet get vms,hosts 'web*'

  # One VM and one host, by KIND/NAME
  vsfleet get vm/web-01 host/esxi-07

  # Find a name anywhere in the estate, in every kind
  vsfleet get all '*golden*' -A

  # The same VMs, as the capture labelled nightly recorded them
  vsfleet get vms -A --run nightly

  # Production VMs with eight or more vCPUs, biggest first
  vsfleet get vms -l env=prod --field-selector 'cpu>=8' --sort-by .cpu

  # Names only, to feed describe
  vsfleet get vms -l env=prod -o name

  # Chosen columns, without a header
  vsfleet get vms -o custom-columns=NAME:.name,HOST:.host --no-headers

  # One field per line
  vsfleet get vms -o jsonpath='{range .items[*]}{.name}{"\n"}{end}'

  # HBAs on one host, as CSV
  vsfleet get hbas --host esxi-07 -o csv

  # Re-read datastores every minute
  vsfleet get datastores -w --refresh 1m`,
		Annotations: map[string]string{annotationAllOutputs: "true"},
		// parseResourceArgs reports a missing KIND with where to find one.
		Args: cobra.ArbitraryArgs,
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return append(gettableKindNames(), "all"), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := f.rejectRemoved(cmd); err != nil {
				return err
			}
			reqs, err := parseResourceArgs(args)
			if err != nil {
				return err
			}
			spec, err := parseOutput(a.Format)
			if err != nil {
				return err
			}
			opt, err := f.options(spec)
			if err != nil {
				return err
			}
			for i, s := range scopeFlags {
				if scopes[i] != "" {
					opt.scopes[s.flag] = scopes[i]
				}
			}
			if cmd.Flags().Changed("refresh") && !f.watch {
				return errors.New("--refresh applies with -w")
			}
			if f.run != "" {
				if f.watch {
					return errors.New("-w re-reads vCenter; a capture --run names never changes")
				}
				if err := a.loadCapture(cmd.Context(), f.run); err != nil {
					return err
				}
			}
			g := getRun{a: a, reqs: reqs, opt: opt, spec: spec, noHeaders: f.noHeaders}
			if f.watch {
				return g.watch(cmd.Context())
			}
			return g.once(cmd.Context())
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.fieldSelector, "field-selector", "", "select by field, e.g. 'cpu>=8,power_state=poweredOn'")
	fl.StringVarP(&f.labelSelector, "selector", "l", "", "select by vSphere tag, CATEGORY=NAME, e.g. env=prod or 'env in (prod,dev)'")
	fl.StringVar(&f.sortBy, "sort-by", "", "sort by a JSONPath expression, e.g. .name or .cpu")
	fl.BoolVar(&f.noHeaders, "no-headers", false, "leave out the header row of table, wide, csv and custom-columns output")
	fl.BoolVarP(&f.watch, "watch", "w", false, "re-read and re-render on the refresh interval until interrupted")
	fl.StringVar(&f.run, "run", "", "read a stored capture instead of vCenter: ID, label, latest, previous or date")
	fl.DurationVar(&a.RefreshInterval, "refresh", 0, "with -w, how often to re-read (0 for the interface's default of "+tui.DefaultRefreshInterval.String()+")")
	for i, s := range scopeFlags {
		fl.StringVar(&scopes[i], s.flag, "", s.usage)
	}
	fl.StringArrayVar(&f.where, "where", nil, "removed: use --field-selector and -l")
	fl.BoolVar(&f.wide, "wide", false, "removed: use -o wide")
	_ = fl.MarkHidden("where")
	_ = fl.MarkHidden("wide")
	return cmd
}

// rejectRemoved answers the spellings #501 replaced with where they went.
func (f getFlags) rejectRemoved(cmd *cobra.Command) error {
	switch {
	case cmd.Flags().Changed("where"):
		return errors.New(`--where was renamed: use --field-selector 'cpu>=8,...' for fields and -l CATEGORY=NAME for tags`)
	case cmd.Flags().Changed("wide"):
		return errors.New(`--wide was renamed: use -o wide`)
	}
	return nil
}

// options parses the selector and sort flags once, before any vCenter is
// asked, so a typo fails fast.
func (f getFlags) options(spec outputSpec) (getOptions, error) {
	opt := getOptions{fields: f.fieldSelector, wide: spec.format == FormatWide, scopes: map[string]string{}}
	if f.labelSelector != "" {
		var err error
		if opt.labels, err = query.ParseLabelSelector(f.labelSelector); err != nil {
			return opt, fmt.Errorf("-l: %w", err)
		}
	}
	if f.sortBy != "" {
		expr, err := jsonpath.ParseExpr(f.sortBy)
		if err != nil {
			return opt, fmt.Errorf("--sort-by: %w", err)
		}
		opt.sortBy = expr
	}
	switch spec.format {
	case FormatJSONPath:
		if _, err := jsonpath.Parse(spec.arg); err != nil {
			return opt, err
		}
	case FormatCustomColumns:
		if _, err := parseCustomColumns(spec.arg); err != nil {
			return opt, err
		}
	}
	return opt, nil
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

// getRun is one "get" invocation: what to list and how to print it.
type getRun struct {
	a         *App
	reqs      []resourceRequest
	opt       getOptions
	spec      outputSpec
	noHeaders bool
}

func (g getRun) once(ctx context.Context) error {
	listings, failures, err := g.list(ctx)
	if err != nil {
		reportFailures(g.a, failures)
		return err
	}
	defer reportFailures(g.a, failures)
	defer reportWarnings(g.a, listings)
	if err := render(g.a.out(), g.spec, listings, g.noHeaders, g.single()); err != nil {
		return err
	}
	missing := missingError(listings)
	if missing == nil && (g.spec.format == FormatTable || g.spec.format == FormatWide) {
		if !slices.ContainsFunc(listings, func(l *kindListing) bool { return len(l.rows) > 0 }) {
			// On stderr, so a script reading stdout sees an empty answer.
			fmt.Fprintln(g.a.errOut(), "No resources found.")
		}
	}
	return missing
}

// reportWarnings prints each listing's metadata warnings, once across all of
// them: every kind reads tags the same way, so one failure would otherwise
// repeat for each kind listed.
func reportWarnings(a *App, listings []*kindListing) {
	var seen []string
	for _, l := range listings {
		for _, w := range l.warnings {
			if !slices.Contains(seen, w) {
				seen = append(seen, w)
				fmt.Fprintf(a.errOut(), "%s %s\n", glyphFail, w)
			}
		}
	}
}

// single reports whether exactly one object was asked for by name.
func (g getRun) single() bool {
	return len(g.reqs) == 1 && len(g.reqs[0].names) == 1 && !isGlob(g.reqs[0].names[0])
}

func (g getRun) list(ctx context.Context) ([]*kindListing, []contextFailure, error) {
	type pending struct {
		req    resourceRequest
		lister kindLister
	}
	work := make([]pending, 0, len(g.reqs))
	for _, r := range g.reqs {
		l, err := listerFor(r.kind)
		if err != nil {
			return nil, nil, err
		}
		work = append(work, pending{r, l})
	}
	// A scope flag that no requested kind takes is a mistake, not a no-op:
	// "get vms --host esxi-07" would otherwise list every VM.
	for flag := range g.opt.scopes {
		if !slices.ContainsFunc(work, func(p pending) bool { return p.lister.scope() == flag }) {
			return nil, nil, fmt.Errorf("--%s applies to %s only", flag, strings.Join(kindsWithScope(flag), ", "))
		}
	}
	if g.spec.format == FormatCSV && len(work) > 1 {
		return nil, nil, errors.New("-o csv takes one kind, so every row has the same columns")
	}
	var (
		listings []*kindListing
		failures contextFailures
	)
	for _, p := range work {
		l, failed, err := p.lister.list(ctx, g.a, p.req.names, g.opt)
		failures.add(failed)
		if err != nil {
			return nil, failures.list, err
		}
		l.kind, l.all = p.req.kind, p.req.all
		listings = append(listings, l)
	}
	return listings, failures.list, nil
}

// watch re-renders on the refresh interval until interrupted. On a terminal
// each render replaces the last; into a pipe or file each one is appended, a
// blank line apart, so a log keeps the history. Interrupting ends the watch
// cleanly, which lets the vCenter sessions be logged out.
func (g getRun) watch(ctx context.Context) error {
	interval := g.a.RefreshInterval
	switch {
	case interval == 0:
		interval = tui.DefaultRefreshInterval
	case interval < 0:
		return errors.New("-w needs a positive --refresh interval")
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	clear := isTerminalWriter(g.a.out())
	for first := true; ; first = false {
		switch {
		case clear:
			fmt.Fprint(g.a.out(), "\x1b[H\x1b[2J")
		case !first:
			fmt.Fprintln(g.a.out())
		}
		err := g.once(ctx)
		if ctx.Err() != nil {
			return nil
		}
		// Only the first render may fail the watch: after that, an estate
		// that drops out is reported and read again next time.
		if err != nil {
			if first {
				return err
			}
			fmt.Fprintf(g.a.errOut(), "%s %v\n", glyphFail, err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

// missingError reports the exact names that matched nothing. A name asked of
// "all" is missing only when every kind it stands for lacks it, and is
// reported once.
func missingError(listings []*kindListing) error {
	var (
		errs    []error
		fromAll []string
		misses  = map[string]int{}
		kinds   int
	)
	for _, l := range listings {
		if l.all {
			kinds++
		}
		for _, name := range l.missing {
			if !l.all {
				errs = append(errs, &noMatchError{kind: l.kind.Name, query: name})
				continue
			}
			if misses[name] == 0 {
				fromAll = append(fromAll, name)
			}
			misses[name]++
		}
	}
	for _, name := range fromAll {
		if misses[name] == kinds {
			errs = append(errs, &noMatchError{kind: "object", query: name})
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

// maxPorts reads a distributed switch's port limit. vSphere reports a switch
// without one as the largest int32.
func maxPorts(n int32) string {
	if n == math.MaxInt32 {
		return "unlimited"
	}
	return i32toa(n)
}
