package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vc-tui/internal/tui"
	"github.com/easonliuuuuu/vc-tui/internal/uistate"
)

func newUICommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "ui",
		Short: "Browse every vCenter in one terminal interface",
		Long: `Open the terminal interface.

Every configured vCenter appears in the sidebar whether or not it is
reachable, and the resource tabs show one kind at a time for the selected
context or, with --all-contexts, for the whole estate at once.

With no contexts configured yet, this opens straight into the setup form
instead of asking you to run "vctui context add" first.

The context, resource tab and sort order are remembered between runs, so
closing vctui and opening it again picks up where you left off. --context
overrides the remembered context for this run without changing what gets
remembered next time.

The interface shows nothing the command line cannot: it is a faster way to
ask the same questions, not a second implementation of them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUI(a, cmd)
		},
	}
}

// runUI is also what a bare "vctui" runs: the terminal interface is the
// product's front door, and "vctui ui" is kept as an explicit, memorable
// alias for it.
func runUI(a *App, cmd *cobra.Command) error {
	cfg, err := a.Config()
	if err != nil {
		return err
	}

	remembered := uistate.Load("")
	current := cfg.CurrentContext
	if remembered.Context != "" {
		if _, err := cfg.Context(remembered.Context); err == nil {
			current = remembered.Context
		}
	}

	// The sidebar is a switcher, so it always lists every context.
	// --context and --all-contexts choose where the cursor starts and how
	// wide the initial scope is, not what exists. An explicit --context
	// overrides the remembered one for this run only.
	if len(a.ContextNames) > 0 {
		if len(cfg.Contexts) == 0 {
			return fmt.Errorf("context %q does not exist (no contexts are configured yet)", a.ContextNames[0])
		}
		if _, err := cfg.Context(a.ContextNames[0]); err != nil {
			return err
		}
		current = a.ContextNames[0]
	}

	backend := tui.NewBackend(cfg, a.Resolver(), a.Sessions(), a.ConnectOptions())
	snap, runErr := tui.Run(cmd.Context(), backend, tui.Options{
		Current:     current,
		AllContexts: a.AllContexts,
		Kind:        remembered.Kind,
		Sort:        remembered.Sort,
	})
	// A clean run is the only one worth remembering: a program that never
	// really started (no TTY, say) has nothing truthful to say about where
	// the cursor was.
	if runErr == nil {
		if err := uistate.Save("", uistate.State{Context: snap.Context, Kind: snap.Kind, Sort: snap.Sort}); err != nil {
			fmt.Fprintf(a.errOut(), "warning: could not remember the last screen (%v)\n", err)
		}
	}
	return runErr
}
