package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func newResourcePoolListCommand(a *App) *cobra.Command {
	return listCommand(a, "List resource pools", func(cmd *cobra.Command, f *listFlags) error {
		if err := f.prepare(vsphere.KindResourcePool); err != nil {
			return err
		}
		pools, failures, err := gather(cmd.Context(), a, func(ctx context.Context, c *vsphere.Client) ([]vsphere.ResourcePool, error) {
			return c.ListResourcePools(ctx)
		})
		if err != nil {
			reportFailures(a, failures)
			return err
		}
		reportMetadataWarnings(a, pools)
		pools = filterSlice(pools, func(p vsphere.ResourcePool) bool { return f.matchesObject(vsphere.KindResourcePool, p, p.Name) })
		if a.json() {
			defer reportFailures(a, failures)
			return writeJSON(a.out(), pools)
		}
		headers := []string{"NAME", "PARENT", "ROOT", "VMS", "CPU SHARES", "CPU LEVEL", "MEM CONFIGURED", "MEM SHARES", "MEM LEVEL"}
		if f.wide {
			headers = append(headers, "TAGS", "CUSTOM ATTRIBUTES")
		}
		multi := a.multiContext()
		if multi {
			headers = append([]string{"CONTEXT"}, headers...)
		}
		t := newTable(a.out(), headers...)
		for _, p := range pools {
			row := []string{p.Name, dash(p.Parent), yesNo(p.Root), itoa(len(p.VMRefs)), i32toa(p.CPUShares), dash(p.CPULevel), humanMB(p.MemConfiguredMB), i32toa(p.MemShares), dash(p.MemLevel)}
			if f.wide {
				row = append(row, dash(metadataTags(p.Metadata)), dash(metadataAttributes(p.Metadata)))
			}
			if multi {
				row = append([]string{p.Context}, row...)
			}
			t.row(row...)
		}
		t.flush()
		reportFailures(a, failures)
		return nil
	})
}
