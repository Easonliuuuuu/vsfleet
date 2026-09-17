package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func newDVSwitchListCommand(a *App) *cobra.Command {
	return listCommand(a, "List distributed virtual switches", func(cmd *cobra.Command, f *listFlags) error {
		if err := f.prepare(vsphere.KindDVSwitch); err != nil {
			return err
		}
		switches, failures, err := gather(cmd.Context(), a, func(ctx context.Context, c *vsphere.Client) ([]vsphere.DVSwitch, error) {
			return c.ListDVSwitches(ctx)
		})
		if err != nil {
			reportFailures(a, failures)
			return err
		}
		reportMetadataWarnings(a, switches)
		switches = filterSlice(switches, func(d vsphere.DVSwitch) bool { return f.matchesObject(vsphere.KindDVSwitch, d, d.Name) })
		if a.json() {
			defer reportFailures(a, failures)
			return writeJSON(a.out(), switches)
		}
		headers := []string{"NAME", "VENDOR", "VERSION", "PORTS", "MAX PORTS", "HOSTS", "PORT GROUPS"}
		if f.wide {
			headers = append(headers, "TAGS", "CUSTOM ATTRIBUTES")
		}
		multi := a.multiContext()
		if multi {
			headers = append([]string{"CONTEXT"}, headers...)
		}
		t := newTable(a.out(), headers...)
		for _, d := range switches {
			row := []string{d.Name, dash(d.Vendor), dash(d.Version), i32toa(d.NumPorts), i32toa(d.MaxPorts), itoa(len(d.Hosts)), itoa(len(d.PortGroups))}
			if f.wide {
				row = append(row, dash(metadataTags(d.Metadata)), dash(metadataAttributes(d.Metadata)))
			}
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

func newDVPortGroupListCommand(a *App) *cobra.Command {
	var switchName string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List distributed port groups",
		Example: `  # Port groups on one distributed switch
  vsfleet dvportgroup list --switch dvs-prod

  # Every distributed port group on the current context, as JSON
  vsfleet dvportgroup list -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			switches, failures, err := gather(cmd.Context(), a, func(ctx context.Context, c *vsphere.Client) ([]vsphere.DVSwitch, error) {
				return c.ListDVSwitches(ctx)
			})
			if err != nil {
				reportFailures(a, failures)
				return err
			}
			if switchName != "" {
				matches := filterByName(switches, switchName, func(d vsphere.DVSwitch) string { return d.Name })
				sw, rerr := exactlyOne(a, "dvswitch", switchName, matches,
					func(d vsphere.DVSwitch) string { return d.Context },
					func(d vsphere.DVSwitch) string { return d.Path },
					func(d vsphere.DVSwitch) string { return d.Name })
				if rerr != nil {
					reportFailures(a, failures)
					return rerr
				}
				switches = []vsphere.DVSwitch{sw}
			}
			if a.json() {
				type wrapped struct {
					Context string `json:"context"`
					Switch  string `json:"switch"`
					vsphere.DVPortGroup
				}
				var rows []wrapped
				for _, d := range switches {
					for _, pg := range d.PortGroups {
						rows = append(rows, wrapped{Context: d.Context, Switch: d.Name, DVPortGroup: pg})
					}
				}
				defer reportFailures(a, failures)
				return writeJSON(a.out(), rows)
			}
			multi := a.multiContext()
			headers := []string{"SWITCH", "NAME", "TYPE", "VLAN", "PORTS", "UPLINK"}
			if multi {
				headers = append([]string{"CONTEXT"}, headers...)
			}
			t := newTable(a.out(), headers...)
			for _, d := range switches {
				for _, pg := range d.PortGroups {
					row := []string{d.Name, pg.Name, dash(pg.Type), dash(pg.VLAN), i32toa(pg.NumPorts), yesNo(pg.Uplink)}
					if multi {
						row = append([]string{d.Context}, row...)
					}
					t.row(row...)
				}
			}
			t.flush()
			reportFailures(a, failures)
			return nil
		},
	}
	cmd.Flags().StringVar(&switchName, "switch", "", "only include port groups on this distributed switch, by name")
	return cmd
}
