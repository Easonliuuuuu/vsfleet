package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/demo"
	"github.com/easonliuuuuu/vsfleet/internal/tui"
)

func newDemoCommand(a *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Explore a sample estate without connecting to a vCenter",
		Example: `  # Open the interface on sample data
  vsfleet demo

  # Same, without the background refresh
  vsfleet demo --refresh -1`,
		Long: `Open the terminal interface on invented inventory.

The estate is three vCenters: two healthy sites reached by different routes
and one disaster-recovery site whose proxy refuses the connection. That last
one is the point — healthy results stay usable while another vCenter is down.

The main site is sized like production: about 1,000 VMs across six clusters,
36 datastores, 24 vApps (some nested), resource pools and 30 networks. The
second healthy site has about 180 VMs. Everything is generated
deterministically, so every run shows the same estate.

The startup animation plays on each launch unless VSFLEET_NO_WELCOME or CI
is set. On Contexts, n previews the connection form; tests, certificate
discovery and saving are disabled. Escape discards the preview.

Nothing here touches your machine or your network. The demo reads no
configuration file, opens no keyring, resolves no credentials, dials nothing,
and writes nothing back: it does not remember the last screen the way a real
run does. The header says DEMO on every screen so a screenshot cannot be
mistaken for a live estate.

The History pane holds five seeded, in-memory assessments, dated May to
September, so diffs, trends, churn and capacity have real history. The newest
includes intentionally unhealthy orphaned-VM and zombie-VMDK evidence. Nothing
is written to disk.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDemo(a, cmd)
		},
	}
	addRefreshFlag(cmd, a)
	return cmd
}

// runDemo is deliberately a fraction of runUI. Every side-effecting
// dependency on App is a lazy accessor, so the demo's promise to leave the
// operator's machine alone is kept by using only the synthetic backend and an
// in-memory assessment store: no Config, Resolver, Sessions, or uistate load
// or save.
func runDemo(a *App, cmd *cobra.Command) error {
	backend, opts, closeHistory, err := demo.Launch(os.Getenv)
	if err != nil {
		return err
	}
	defer closeHistory()
	opts.RefreshInterval = a.RefreshInterval
	opts.In, opts.Out = a.in(), a.out()
	_, err = tui.Run(cmd.Context(), backend, opts)
	return err
}
