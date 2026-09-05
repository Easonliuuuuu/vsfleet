package cli

import (
	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/demo"
	"github.com/easonliuuuuu/vsfleet/internal/tui"
)

func newDemoCommand(a *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Explore a sample estate without connecting to a vCenter",
		Long: `Open the terminal interface on invented inventory.

The estate is three vCenters: two healthy sites reached by different routes
and one disaster-recovery site whose proxy refuses the connection. That last
one is the point — healthy results stay usable while another vCenter is down.

Nothing here touches your machine or your network. The demo reads no
configuration file, opens no keyring, resolves no credentials, dials nothing,
and writes nothing back: it does not remember the last screen the way a real
run does. The header says DEMO on every screen so a screenshot cannot be
mistaken for a live estate.

Historical assessments are unavailable in the demo, since there is no captured
run behind the sample data to compare against.`,
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
// operator's machine alone is kept by not reaching for them: no Config, no
// Resolver, no Sessions, no Assessment, and no uistate load or save.
func runDemo(a *App, cmd *cobra.Command) error {
	_, err := tui.Run(cmd.Context(), demo.NewBackend(), tui.Options{
		Current:         "prod-vc",
		Demo:            true,
		RefreshInterval: a.RefreshInterval,
		In:              a.in(),
		Out:             a.out(),
	})
	return err
}
