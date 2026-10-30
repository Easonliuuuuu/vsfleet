package cli

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/easonliuuuuu/vsfleet/internal/apiresource"
	"github.com/easonliuuuuu/vsfleet/internal/session"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// contextFailure records that one vCenter could not answer. Listing commands
// print the rows they did get and report these separately.
type contextFailure struct {
	Context string
	Err     error
	// Gap marks a context that answered, with an answer known to be
	// incomplete for one kind.
	Gap bool
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
			// One deadline covers connecting and the listing that follows: a
			// vCenter that connects quickly but then hangs enumerating must
			// still be cut off at --timeout, not left to run unbounded once
			// the connection itself succeeded.
			opCtx, cancel, tracker := mgr.Operation(ctx)
			defer cancel()
			s, err := mgr.Connect(opCtx, cc)
			if err != nil {
				results[i] = result{err: mgr.TimeoutError(err, tracker)}
				return nil
			}
			items, err := fn(opCtx, s.Client())
			results[i] = result{items: items, err: mgr.TimeoutError(err, tracker)}
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
		return nil, failures, errNoContextAnswered
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
	if a.capture != nil && !a.AllContexts && len(a.ContextNames) <= 1 {
		return len(a.capture.Contexts) > 1
	}
	if a.AllContexts || len(a.ContextNames) > 1 {
		return true
	}
	cfg, err := a.Config()
	if err != nil {
		return false
	}
	return len(a.ContextNames) == 0 && cfg.CurrentContext == "" && len(cfg.Contexts) > 1
}

func metadataTags(m vsphere.Metadata) string {
	parts := make([]string, 0, len(m.Tags))
	for _, tag := range m.Tags {
		label := tag.Name
		if tag.Category != "" {
			label = tag.Category + "/" + label
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, ",")
}

func metadataAttributes(m vsphere.Metadata) string {
	parts := make([]string, 0, len(m.CustomAttributes))
	for _, attr := range m.CustomAttributes {
		parts = append(parts, attr.Name+"="+attr.Value)
	}
	return strings.Join(parts, ",")
}

// metadataWarning describes a metadata source that was not read completely.
// A source that was never part of the evidence (empty status) is silent here;
// stored-capture readers report it as not recorded instead.
func metadataWarning(source, status, message string) (string, bool) {
	if status == "" || status == vsphere.MetadataAvailable || status == vsphere.MetadataNotApplicable {
		return "", false
	}
	label := strings.ReplaceAll(status, "_", " ")
	if message == "" {
		return fmt.Sprintf("metadata %s %s", source, label), true
	}
	return fmt.Sprintf("metadata %s %s: %s", source, label, message), true
}

// metadataWarnings lists, once each, the metadata sources the objects in
// values were not read completely from.
func metadataWarnings(values any) []string {
	rv := reflect.ValueOf(values)
	if rv.Kind() != reflect.Slice {
		return nil
	}
	var out []string
	for i := 0; i < rv.Len(); i++ {
		m, ok := objectMetadata(rv.Index(i).Interface())
		if !ok {
			continue
		}
		for _, item := range [][3]string{{"tags", m.TagsStatus, m.TagsError}, {"custom attributes", m.CustomAttributesStatus, m.CustomAttributesError}} {
			source, status, message := item[0], item[1], item[2]
			if line, ok := metadataWarning(source, status, message); ok && !slices.Contains(out, line) {
				out = append(out, line)
			}
		}
	}
	return out
}

func objectMetadata(value any) (vsphere.Metadata, bool) {
	switch v := value.(type) {
	case vsphere.VM:
		return v.Metadata, true
	case vsphere.Host:
		return v.Metadata, true
	case vsphere.Cluster:
		return v.Metadata, true
	case vsphere.VApp:
		return v.Metadata, true
	case vsphere.Datastore:
		return v.Metadata, true
	case vsphere.Network:
		return v.Metadata, true
	case vsphere.ResourcePool:
		return v.Metadata, true
	case vsphere.DVSwitch:
		return v.Metadata, true
	default:
		return vsphere.Metadata{}, false
	}
}

// newKindCommands builds the type-first groups that still hold commands with
// no kubectl verb yet: datastore file browsing. Their listing and detail
// commands moved to "get" and "describe" and answer only with where they
// went; every other type-first group is replaced outright by
// renamedKindCommand.
func newKindCommands(a *App) []*cobra.Command {
	return []*cobra.Command{kindGroup("datastore", "Datastores: browse and search files (read-only)", newDatastoreFileCommands(a)...)}
}

// kindGroup is a type-first parent that keeps hidden "list" and "show"
// stubs, so a habit or script gets a pointer to "get" and "describe".
func kindGroup(name, short string, subs ...*cobra.Command) *cobra.Command {
	cmd := requireSubcommand(&cobra.Command{Use: name, Aliases: kindAliases(name), Short: short})
	cmd.AddCommand(subs...)
	k := apiresource.Get(name)
	// "datastore ls" is a real command, so the "list" stub keeps "ls" only
	// where no subcommand has taken it.
	listAliases := []string{"ls"}
	var examples []string
	for _, sub := range subs {
		if sub.Name() == "ls" {
			listAliases = nil
		}
		if !sub.Hidden {
			examples = append(examples, sub.Example)
		}
	}
	cmd.AddCommand(
		renamedCommand("list", listAliases, fmt.Sprintf(`use "vsfleet get %s"`, k.Plural)),
		renamedCommand("show", nil, fmt.Sprintf(`use "vsfleet describe %s NAME"`, k.Name)),
	)
	cmd.Example = strings.Join(examples, "\n\n")
	return cmd
}

// renamedKindCommand answers a removed type-first group, such as
// "vsfleet host list" or "vsfleet host hba list --host esxi-07", with the
// "get" or "describe" command that replaced it. subs holds the hint for each
// of the group's other subcommands, by name.
func renamedKindCommand(name string, subs map[string]string) *cobra.Command {
	k := apiresource.Get(name)
	cmd := renamedCommand(name, kindAliases(name), fmt.Sprintf(`use "vsfleet get %s"`, k.Plural))
	generic := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		// Flag parsing is off, so args can hold flags given before the
		// command too ("--db x host list"): look for the first word
		// that names what was meant rather than trusting args[0].
		for _, arg := range args {
			if hint, ok := subs[arg]; ok {
				return fmt.Errorf("%q was renamed: %s", name+" "+arg, hint)
			}
			switch arg {
			case "list", "ls":
				return fmt.Errorf("%q was renamed: use \"vsfleet get %s\"", name+" "+arg, k.Plural)
			case "show":
				return fmt.Errorf("%q was renamed: use \"vsfleet describe %s NAME\"", name+" show", k.Name)
			}
			if sub, ok := apiresource.Lookup(arg); ok && name == "host" {
				return fmt.Errorf("%q was renamed: use \"vsfleet get %s --host HOST\"", "host "+arg, sub.Plural)
			}
		}
		return generic(c, args)
	}
	return cmd
}

// removedKindGroups are the type-first groups that are gone, with the hint
// for each subcommand they held besides list and show.
var removedKindGroups = []struct {
	name string
	subs map[string]string
}{
	{"vm", map[string]string{
		"events":             `use "vsfleet events --for vm/NAME"`,
		"history":            `use "vsfleet describe vm NAME --history"`,
		"decommission-check": `use "vsfleet analyze decommission vm/NAME"`,
	}},
	{"template", nil}, {"host", nil}, {"cluster", nil}, {"vapp", nil},
	{"network", map[string]string{"compare": `use "vsfleet analyze readiness network cl/SOURCE cl/TARGET -o wide"`}},
	{"dvswitch", nil}, {"dvportgroup", nil}, {"resourcepool", nil}, {"snapshot", nil},
}

// kindAliases returns every spelling of a registered kind except its
// canonical name, so a command and a --kind flag accept the same words.
func kindAliases(name string) []string {
	return apiresource.Get(name).Names()[1:]
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
