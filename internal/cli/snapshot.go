package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func newSnapshotListCommand(a *App) *cobra.Command {
	var vmName string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List VM snapshots",
		Long: strings.TrimSpace(`
List every snapshot vsfleet collected, across every VM, or narrowed to one
VM with --vm.`),
		Example: `  # Every snapshot on the current context
  vsfleet snapshot list

  # Snapshots on one VM
  vsfleet snapshot list --vm web-01

  # As JSON, for a script
  vsfleet snapshot list -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			vms, failures, err := gather(cmd.Context(), a, func(ctx context.Context, c *vsphere.Client) ([]vsphere.VM, error) {
				return c.ListVMs(ctx)
			})
			if err != nil {
				reportFailures(a, failures)
				return err
			}
			if vmName != "" {
				vm, rerr := resolveVM(a, "vm", vmName, vms)
				if rerr != nil {
					reportFailures(a, failures)
					return rerr
				}
				vms = []vsphere.VM{vm}
			}
			if a.json() {
				type wrapped struct {
					Context string `json:"context"`
					VM      string `json:"vm"`
					vsphere.VMSnapshot
				}
				var rows []wrapped
				for _, v := range vms {
					for _, s := range v.Snapshots {
						rows = append(rows, wrapped{Context: v.Context, VM: v.Name, VMSnapshot: s})
					}
				}
				defer reportFailures(a, failures)
				return writeJSON(a.out(), rows)
			}
			multi := a.multiContext()
			headers := []string{"VM", "NAME", "CREATED", "POWER STATE", "QUIESCED", "CURRENT"}
			if multi {
				headers = append([]string{"CONTEXT"}, headers...)
			}
			t := newTable(a.out(), headers...)
			for _, v := range vms {
				for _, s := range v.Snapshots {
					row := []string{v.Name, s.Name, formatSnapshotTime(s.CreateTime), dash(s.PowerState), yesNo(s.Quiesced), yesNo(s.Current)}
					if multi {
						row = append([]string{v.Context}, row...)
					}
					t.row(row...)
				}
			}
			t.flush()
			reportFailures(a, failures)
			return nil
		},
	}
	cmd.Flags().StringVar(&vmName, "vm", "", "only include snapshots on this VM, by name or UUID")
	return cmd
}
