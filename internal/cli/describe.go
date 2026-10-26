package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/apiresource"
	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/humanize"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func newDescribeCommand(a *App) *cobra.Command {
	var (
		run     string
		history historyFlags
	)
	cmd := &cobra.Command{
		Use:   "describe KIND NAME... | KIND/NAME...",
		Short: "Show one object's detailed evidence, live or from a capture",
		Long: strings.TrimSpace(`
Show an object's full collected evidence, the way "kubectl describe" does:
identity, configuration, capacity and, for a VM or template, every disk, NIC
and snapshot vsfleet collected for it.

KIND is one of ` + strings.Join(describableKinds(), ", ") + `. NAME is
the object's name, ignoring case; a VM or template may also be given by
instance or BIOS UUID, which survives a rename. A name that matches objects
on several contexts lists them and fails; narrow with -n.

Host storage and network subresources are listed with "vsfleet get", e.g.
"vsfleet get hbas --host NAME", so describing a host never costs more than
listing hosts already does.

A capture is named the way --run names one: an ID, a label, latest, previous
or a date. Describing it shows what it recorded, per context and kind, read
from the history database; -n narrows that to some contexts.

Any other object is read live from vCenter. --run reads it from a stored
capture instead, the way "vsfleet get --run" does. --history shows how a VM changed
across every stored capture rather than how it is now; it reads stored
evidence only and never contacts vCenter.`),
		Example: `  # Describe a VM on the current context
  vsfleet describe vm web-01

  # The same, in kubectl's KIND/NAME form
  vsfleet describe vm/web-01

  # Narrow to one context when the name is ambiguous
  vsfleet describe host esxi-07 -n prod

  # By instance UUID, which survives a rename
  vsfleet describe vm 5029c07a-1b3e-4d2f-9c11-8a7e6f0d4b52

  # As JSON, for a script
  vsfleet describe datastore nvme-01 -o json

  # The VM as the capture labelled pre-migration recorded it
  vsfleet describe vm web-01 --run pre-migration

  # What the most recent capture recorded, and where it has gaps
  vsfleet describe capture latest

  # Every recorded change to a VM, including unchanged captures
  vsfleet describe vm web-01 --history --all-observations`,
		Args: cobra.MinimumNArgs(1),
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return describableKinds(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			reqs, err := parseResourceArgs(args)
			if err != nil {
				return err
			}
			if err := history.check(cmd, run); err != nil {
				return err
			}
			for _, r := range reqs {
				if r.all {
					return errors.New(`"all" lists with get; describe takes one KIND`)
				}
				if _, ok := describers[r.kind.Name]; !ok {
					return fmt.Errorf("%s cannot be described (supported: %s)", r.kind.Plural, strings.Join(describableKinds(), ", "))
				}
				if len(r.names) == 0 {
					return fmt.Errorf("describe needs a NAME: \"vsfleet describe %s NAME\"", r.kind.Name)
				}
			}
			if history.on {
				return describeHistory(cmd.Context(), a, reqs, history)
			}
			if run != "" {
				if err := a.loadCapture(cmd.Context(), run); err != nil {
					return err
				}
			}
			return runDescribe(cmd.Context(), a, reqs)
		},
	}
	cmd.Flags().StringVar(&run, "run", "", "read a stored capture instead of vCenter: ID, label, latest, previous or date")
	history.add(cmd)
	return cmd
}

// describer resolves and prints objects of one kind.
type describer interface {
	resolve(ctx context.Context, a *App, names []string) ([]any, []contextFailure, error)
	print(out io.Writer, object any)
}

// detail describes objects of one kind whose evidence is T.
type detail[T any] struct {
	// kind is the capture collection that records T.
	kind   string
	fetch  func(context.Context, *vsphere.Client) ([]T, error)
	stored func(*assessment.ExportData) ([]T, error)
	find   func(a *App, query string, items []T) (T, error)
	show   func(io.Writer, T)
}

func (d detail[T]) resolve(ctx context.Context, a *App, names []string) ([]any, []contextFailure, error) {
	items, failures, err := read(ctx, a, d.kind, d.fetch, d.stored)
	if err != nil {
		return nil, failures, err
	}
	var (
		found []any
		errs  []error
	)
	for _, name := range names {
		v, err := d.find(a, name, items)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		found = append(found, v)
	}
	return found, failures, errors.Join(errs...)
}

func (d detail[T]) print(out io.Writer, object any) { d.show(out, object.(T)) }

// byName resolves an object by exact name, ignoring case, for kinds that
// have no other identifier.
func byName[T any](kind string, nameOf, contextOf, pathOf func(T) string) func(*App, string, []T) (T, error) {
	return func(a *App, query string, items []T) (T, error) {
		return exactlyOne(a, kind, query, filterByName(items, query, nameOf), contextOf, pathOf, nameOf)
	}
}

var describers = map[string]describer{
	"vm": detail[vsphere.VM]{
		kind:   "vm",
		fetch:  func(ctx context.Context, c *vsphere.Client) ([]vsphere.VM, error) { return c.ListVMs(ctx) },
		stored: storedVMs(false),
		find:   func(a *App, q string, items []vsphere.VM) (vsphere.VM, error) { return resolveVM(a, "vm", q, items) },
		show:   printVMDetail,
	},
	"template": detail[vsphere.VM]{
		kind:   "vm",
		fetch:  func(ctx context.Context, c *vsphere.Client) ([]vsphere.VM, error) { return c.ListTemplates(ctx) },
		stored: storedVMs(true),
		find: func(a *App, q string, items []vsphere.VM) (vsphere.VM, error) {
			return resolveVM(a, "template", q, items)
		},
		show: printVMDetail,
	},
	"host": detail[vsphere.Host]{
		kind:   "host",
		fetch:  func(ctx context.Context, c *vsphere.Client) ([]vsphere.Host, error) { return c.ListHosts(ctx) },
		stored: storedObjects[vsphere.Host](vsphere.KindHost),
		find: byName("host", func(h vsphere.Host) string { return h.Name },
			func(h vsphere.Host) string { return h.Context }, func(h vsphere.Host) string { return h.Path }),
		show: printHostDetail,
	},
	"cluster": detail[vsphere.Cluster]{
		kind:   "cluster",
		fetch:  func(ctx context.Context, c *vsphere.Client) ([]vsphere.Cluster, error) { return c.ListClusters(ctx) },
		stored: storedObjects[vsphere.Cluster](vsphere.KindCluster),
		find: byName("cluster", func(c vsphere.Cluster) string { return c.Name },
			func(c vsphere.Cluster) string { return c.Context }, func(c vsphere.Cluster) string { return c.Path }),
		show: printClusterDetail,
	},
	"vapp": detail[vsphere.VApp]{
		kind:   "vapp",
		fetch:  func(ctx context.Context, c *vsphere.Client) ([]vsphere.VApp, error) { return c.ListVApps(ctx) },
		stored: storedObjects[vsphere.VApp](vsphere.KindVApp),
		find: byName("vapp", func(v vsphere.VApp) string { return v.Name },
			func(v vsphere.VApp) string { return v.Context }, func(v vsphere.VApp) string { return v.Path }),
		show: printVAppDetail,
	},
	"datastore": detail[vsphere.Datastore]{
		kind: "datastore",
		fetch: func(ctx context.Context, c *vsphere.Client) ([]vsphere.Datastore, error) {
			return c.ListDatastores(ctx)
		},
		stored: storedObjects[vsphere.Datastore](vsphere.KindDatastore),
		find: byName("datastore", func(d vsphere.Datastore) string { return d.Name },
			func(d vsphere.Datastore) string { return d.Context }, func(d vsphere.Datastore) string { return d.Path }),
		show: printDatastoreDetail,
	},
	"network": detail[vsphere.Network]{
		kind:   "network",
		fetch:  func(ctx context.Context, c *vsphere.Client) ([]vsphere.Network, error) { return c.ListNetworks(ctx) },
		stored: storedObjects[vsphere.Network](vsphere.KindNetwork),
		find: byName("network", func(n vsphere.Network) string { return n.Name },
			func(n vsphere.Network) string { return n.Context }, func(n vsphere.Network) string { return n.Path }),
		show: printNetworkDetail,
	},
	"capture": captureDescriber{},
}

// describableKinds are the kinds "describe" accepts, in registry order.
func describableKinds() []string {
	var out []string
	for _, k := range apiresource.All() {
		if _, ok := describers[k.Name]; ok {
			out = append(out, k.Name)
		}
	}
	return out
}

// runDescribe prints every object asked for, a blank line apart. A name that
// cannot be resolved is reported after the ones that could, the way kubectl
// describes what it found before failing on what it did not.
func runDescribe(ctx context.Context, a *App, reqs []resourceRequest) error {
	type found struct {
		d       describer
		objects []any
	}
	var (
		all      []found
		failures contextFailures
		errs     []error
	)
	for _, r := range reqs {
		d := describers[r.kind.Name]
		objects, failed, err := d.resolve(ctx, a, r.names)
		failures.add(failed)
		var none *noMatchError
		if err != nil && len(objects) == 0 && !errors.As(err, &none) {
			reportFailures(a, failures.list)
			return err
		}
		if err != nil {
			errs = append(errs, err)
		}
		all = append(all, found{d, objects})
	}
	defer reportFailures(a, failures.list)
	if a.json() {
		var objects []any
		for _, f := range all {
			objects = append(objects, f.objects...)
		}
		switch len(objects) {
		case 0:
		case 1:
			if err := writeJSON(a.out(), objects[0]); err != nil {
				return err
			}
		default:
			if err := writeJSON(a.out(), objects); err != nil {
				return err
			}
		}
		return errors.Join(errs...)
	}
	printed := 0
	for _, f := range all {
		for _, o := range f.objects {
			if printed > 0 {
				fmt.Fprintln(a.out())
			}
			f.d.print(a.out(), o)
			printed++
		}
	}
	return errors.Join(errs...)
}

// filterByName narrows items to those whose name (case-insensitively)
// matches query. It is the first step of resolving "describe KIND NAME" and every
// "--host"/"--switch"/"--vm" style narrowing flag: the second step,
// cardinality, is exactlyOne.
func filterByName[T any](items []T, query string, nameOf func(T) string) []T {
	var out []T
	for _, item := range items {
		if strings.EqualFold(nameOf(item), query) {
			out = append(out, item)
		}
	}
	return out
}

// noMatchError is the error exactlyOne returns when nothing matched, so a
// caller with another place to look can tell it from an ambiguous match.
type noMatchError struct{ kind, query string }

func (e *noMatchError) Error() string { return fmt.Sprintf("no %s matched %q", e.kind, e.query) }

// exactlyOne requires matches to contain exactly one item, printing
// candidates and returning an actionable error otherwise. kind and query
// name the object being resolved (e.g. "host", "esxi-01") for the error
// text; contextOf/pathOf/nameOf describe each candidate for the table
// printed when the match is ambiguous.
func exactlyOne[T any](a *App, kind, query string, matches []T, contextOf, pathOf, nameOf func(T) string) (T, error) {
	var zero T
	switch len(matches) {
	case 0:
		return zero, &noMatchError{kind: kind, query: query}
	case 1:
		return matches[0], nil
	default:
		printCandidates(a.errOut(), matches, contextOf, pathOf, nameOf)
		return zero, fmt.Errorf("%s %q is ambiguous (%d matches); use --context to narrow", kind, query, len(matches))
	}
}

func printCandidates[T any](out io.Writer, matches []T, contextOf, pathOf, nameOf func(T) string) {
	fmt.Fprintln(out, "Candidates:")
	t := newTable(out, "CONTEXT", "PATH", "NAME")
	for _, m := range matches {
		t.row(contextOf(m), dash(pathOf(m)), nameOf(m))
	}
	t.flush()
}

func vmContextOf(v vsphere.VM) string { return v.Context }
func vmPathOf(v vsphere.VM) string    { return v.Path }
func vmNameOf(v vsphere.VM) string    { return v.Name }

// resolveVM finds exactly one VM (or template) matching query by name,
// instance UUID or BIOS UUID, as "describe vm NAME --history" does.
func resolveVM(a *App, kind, query string, vms []vsphere.VM) (vsphere.VM, error) {
	matches := filterSlice(append([]vsphere.VM(nil), vms...), func(v vsphere.VM) bool {
		return strings.EqualFold(v.Name, query) ||
			(v.InstanceUUID != "" && v.InstanceUUID == query) ||
			(v.BIOSUUID != "" && v.BIOSUUID == query)
	})
	return exactlyOne(a, kind, query, matches, vmContextOf, vmPathOf, vmNameOf)
}

// printVMDetail renders a VM or template's full evidence. It is shared by
// "describe vm" and "describe template" since both read the same vsphere.VM shape.
func printVMDetail(out io.Writer, v vsphere.VM) {
	f := newFields(out)
	f.add("Name", v.Name)
	f.add("Context", v.Context)
	f.add("Path", v.Path)
	f.add("ID", v.ID)
	f.add("Instance UUID", v.InstanceUUID)
	f.add("BIOS UUID", v.BIOSUUID)
	f.add("Power state", v.PowerState)
	f.add("Connection state", v.ConnectionState)
	f.add("Guest OS", v.GuestOS)
	f.add("Guest state", v.GuestState)
	f.add("CPU", i32toa(v.CPU))
	f.add("Memory", humanMB(v.MemoryMB))
	f.add("IP address", v.IPAddress)
	f.add("DNS name", v.GuestHostName)
	f.add("Host", v.Host)
	f.add("Cluster", v.Cluster)
	f.add("Folder", v.Folder)
	f.add("Datastores", strings.Join(v.Datastores, ", "))
	if v.StorageGB > 0 {
		f.add("Storage", fmt.Sprintf("%.1f GB", v.StorageGB))
	}
	f.add("Tools state", v.ToolsState)
	f.add("Tools version", v.ToolsVersion)
	f.add("Annotation", v.Annotation)
	f.add("Firmware", v.Firmware)
	f.add("Hardware version", v.HardwareVersion)
	if v.SecureBootEnabled != nil {
		f.add("Secure boot", yesNo(*v.SecureBootEnabled))
	}
	if v.Metadata.TagsStatus == "available" {
		f.add("Tags", dash(metadataTags(v.Metadata)))
	}
	if v.Metadata.CustomAttributesStatus == "available" {
		f.add("Custom attributes", dash(metadataAttributes(v.Metadata)))
	}
	f.flush()

	if len(v.Disks) > 0 {
		fmt.Fprintln(out, "\nDisks:")
		t := newTable(out, "LABEL", "CAPACITY", "CONTROLLER", "BACKING", "THIN")
		for _, d := range v.Disks {
			t.row(d.Label, humanBytes(d.CapacityBytes), dash(d.ControllerLabel), dash(d.BackingType), dashBoolPtr(d.ThinProvisioned))
		}
		t.flush()
	}
	if len(v.NICs) > 0 {
		fmt.Fprintln(out, "\nNICs:")
		t := newTable(out, "LABEL", "ADAPTER", "NETWORK", "MAC", "CONNECTED")
		for _, n := range v.NICs {
			t.row(n.Label, dash(n.Adapter), dash(n.Network), dash(n.MACAddress), dashBoolPtr(n.Connected))
		}
		t.flush()
	}
	if len(v.Snapshots) > 0 {
		fmt.Fprintln(out, "\nSnapshots:")
		t := newTable(out, "NAME", "CREATED", "POWER STATE", "CURRENT")
		for _, s := range v.Snapshots {
			t.row(s.Name, formatSnapshotTime(s.CreateTime), dash(s.PowerState), yesNo(s.Current))
		}
		t.flush()
	}
}

func printHostDetail(out io.Writer, h vsphere.Host) {
	f := newFields(out)
	f.add("Name", h.Name)
	f.add("Context", h.Context)
	f.add("Path", h.Path)
	f.add("ID", h.ID)
	f.add("Cluster", h.Cluster)
	state := h.ConnectionState
	if h.InMaintenance {
		state += " (maintenance)"
	}
	f.add("Power state", h.PowerState)
	f.add("Connection state", state)
	f.add("Vendor", h.Vendor)
	f.add("Model", h.Model)
	f.add("Version", h.Version)
	f.add("Build", h.Build)
	f.add("CPU", fmt.Sprintf("%d cores / %d threads @ %s", h.CPUCores, h.CPUThreads, dash(mhz(int64(h.CPUMHz)))))
	f.add("Total CPU", dash(mhz(h.TotalCPU())))
	f.add("CPU usage", dash(mhz(h.CPUUsageMHz)))
	f.add("Memory", humanMB(h.MemoryMB))
	f.add("Memory usage", humanMB(h.MemoryUsageMB))
	f.add("VMs", itoa(h.VMCount))
	if h.Metadata.TagsStatus == "available" {
		f.add("Tags", dash(metadataTags(h.Metadata)))
	}
	if h.Metadata.CustomAttributesStatus == "available" {
		f.add("Custom attributes", dash(metadataAttributes(h.Metadata)))
	}
	f.flush()
	fmt.Fprintf(out, "\nRun \"vsfleet get hbas,pnics,vswitches,portgroups,vmkernels,multipaths --host %s\" for storage and network subresource evidence.\n", h.Name)
}

func printClusterDetail(out io.Writer, c vsphere.Cluster) {
	f := newFields(out)
	f.add("Name", c.Name)
	f.add("Context", c.Context)
	f.add("Path", c.Path)
	f.add("ID", c.ID)
	f.add("Datacenter", c.Datacenter)
	f.add("Standalone", yesNo(c.Standalone))
	f.add("Hosts", itoa(c.Hosts))
	f.add("Effective hosts", itoa(c.EffectiveHost))
	f.add("CPU cores", i32toa(c.CPUCores))
	f.add("Total CPU", dash(mhz(c.TotalCPUMHz)))
	f.add("Total memory", humanMB(c.TotalMemoryMB))
	f.add("DRS", onOff(c.DRSEnabled))
	f.add("HA", onOff(c.HAEnabled))
	if c.Metadata.TagsStatus == "available" {
		f.add("Tags", dash(metadataTags(c.Metadata)))
	}
	if c.Metadata.CustomAttributesStatus == "available" {
		f.add("Custom attributes", dash(metadataAttributes(c.Metadata)))
	}
	f.flush()
}

func printVAppDetail(out io.Writer, v vsphere.VApp) {
	f := newFields(out)
	f.add("Name", v.Name)
	f.add("Context", v.Context)
	f.add("Path", v.Path)
	f.add("ID", v.ID)
	f.add("Status", v.Status)
	f.add("Datacenter", v.Datacenter)
	f.add("Cluster", v.Cluster)
	f.add("Compute resource", v.ComputeResource)
	f.add("Parent container", v.ParentContainer)
	f.add("Parent vApp", v.ParentVApp)
	f.add("Direct VMs", strings.Join(v.DirectVMs, ", "))
	f.add("Child vApps", strings.Join(v.ChildVApps, ", "))
	f.add("Child resource pools", strings.Join(v.ChildResourcePools, ", "))
	if a := v.Allocation; a != nil {
		f.add("CPU allocation", humanize.Allocation(a.CPULimitMHz, a.CPUReservationMHz, a.CPUExpandable, a.CPULevel, a.CPUShares, humanize.MHz))
		f.add("Memory allocation", humanize.Allocation(a.MemLimitMB, a.MemReservationMB, a.MemExpandable, a.MemLevel, a.MemShares, humanize.MB))
	}
	f.add("Startup order", vsphere.StartOrderText(v.StartOrder))
	if v.Metadata.TagsStatus == "available" {
		f.add("Tags", dash(metadataTags(v.Metadata)))
	}
	if v.Metadata.CustomAttributesStatus == "available" {
		f.add("Custom attributes", dash(metadataAttributes(v.Metadata)))
	}
	f.flush()
}

func printDatastoreDetail(out io.Writer, d vsphere.Datastore) {
	f := newFields(out)
	f.add("Name", d.Name)
	f.add("Context", d.Context)
	f.add("Path", d.Path)
	f.add("ID", d.ID)
	f.add("Datacenter", d.Datacenter)
	f.add("Type", d.Type)
	f.add("Accessible", yesNo(d.Accessible))
	f.add("Maintenance", d.Maintenance)
	f.add("Capacity", humanBytes(d.CapacityBytes))
	f.add("Free", humanBytes(d.FreeBytes))
	if d.CapacityBytes > 0 {
		f.add("Used", fmt.Sprintf("%.0f%%", d.UsedPercent()))
	}
	f.add("Backing URL", d.Backing.URL)
	f.add("VMFS UUID", d.Backing.VMFSUUID)
	f.add("NAS remote", d.Backing.NASRemote)
	if d.Backing.Local {
		f.add("Local", yesNo(true))
	}
	if d.Metadata.TagsStatus == "available" {
		f.add("Tags", dash(metadataTags(d.Metadata)))
	}
	if d.Metadata.CustomAttributesStatus == "available" {
		f.add("Custom attributes", dash(metadataAttributes(d.Metadata)))
	}
	f.flush()
	fmt.Fprintf(out, "\nRun \"vsfleet datastore files list %s\" to browse its contents.\n", d.Name)
}

func printNetworkDetail(out io.Writer, n vsphere.Network) {
	f := newFields(out)
	f.add("Name", n.Name)
	f.add("Context", n.Context)
	f.add("Path", n.Path)
	f.add("ID", n.ID)
	f.add("Datacenter", n.Datacenter)
	f.add("Type", n.Type)
	f.add("Switch", n.Switch)
	f.add("VLAN", n.VLAN)
	f.add("Accessible", yesNo(n.Accessible))
	if n.Metadata.TagsStatus == "available" {
		f.add("Tags", dash(metadataTags(n.Metadata)))
	}
	if n.Metadata.CustomAttributesStatus == "available" {
		f.add("Custom attributes", dash(metadataAttributes(n.Metadata)))
	}
	f.flush()
}

func formatSnapshotTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func dashBoolPtr(b *bool) string {
	if b == nil {
		return "-"
	}
	return yesNo(*b)
}
