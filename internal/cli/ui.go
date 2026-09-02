package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vc-tui/internal/tui"
)

func newUICommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "ui",
		Short: "Browse every vCenter in one terminal interface",
		Long: `Open the terminal interface.

Every configured vCenter appears in the sidebar whether or not it is
reachable, and the resource tabs show one kind at a time for the selected
context or, with --all-contexts, for the whole estate at once.

The interface shows nothing the command line cannot: it is a faster way to
ask the same questions, not a second implementation of them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			if len(cfg.Contexts) == 0 {
				return fmt.Errorf("no contexts configured; run \"vctui context add\" first")
			}
			// The sidebar is a switcher, so it always lists every context.
			// --context and --all-contexts choose where the cursor starts and
			// how wide the initial scope is, not what exists.
			current := cfg.CurrentContext
			if len(a.ContextNames) > 0 {
				if _, err := cfg.Context(a.ContextNames[0]); err != nil {
					return err
				}
				current = a.ContextNames[0]
			}
			backend := tui.NewBackend(cfg.Contexts, a.Sessions(), a.ConnectOptions())
			return tui.Run(cmd.Context(), backend, tui.Options{
				Current:     current,
				AllContexts: a.AllContexts,
			})
		},
	}
}
