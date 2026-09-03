package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/credentials"
	"github.com/easonliuuuuu/vsfleet/internal/transport"
	"github.com/easonliuuuuu/vsfleet/internal/tui"
	"github.com/easonliuuuuu/vsfleet/internal/uistate"
)

// addRefreshFlag registers --refresh on a command that opens the interface.
// It takes the command rather than its flag set so that this package does not
// need to import pflag directly, which would promote it from an indirect
// dependency to a direct one for the sake of one parameter type.
func addRefreshFlag(cmd *cobra.Command, a *App) {
	cmd.Flags().DurationVar(&a.RefreshInterval, "refresh", 0,
		"how often to re-read inventory in the background (0 for the default of "+
			tui.DefaultRefreshInterval.String()+", negative to only read when asked)")
}

func newUICommand(a *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Browse every vCenter in one terminal interface",
		Long: `Open the terminal interface.

The numbered tabs show one resource kind at a time for the vCenter in scope
or, with --all-contexts, for the whole estate at once. Press "c" for the
contexts screen, which lists every configured vCenter whether or not it is
reachable and is where you switch, add, edit and remove them.

"/" filters the table in front of you and "tab" widens the same query into
every vCenter and every kind at once, which is "vsfleet search" without
leaving the interface. A vCenter that has not answered is reported as not
searched rather than quietly narrowing the result.

With no contexts configured yet, this opens straight into the setup form
instead of asking you to run "vsfleet context add" first.

The context, resource tab and sort order are remembered between runs, so
closing vsfleet and opening it again picks up where you left off. --context
overrides the remembered context for this run without changing what gets
remembered next time.

The interface shows nothing the command line cannot: it is a faster way to
ask the same questions, not a second implementation of them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUI(a, cmd)
		},
	}
	addRefreshFlag(cmd, a)
	return cmd
}

// runUI is also what a bare "vsfleet" runs: the terminal interface is the
// product's front door, and "vsfleet ui" is kept as an explicit, memorable
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

	// The contexts screen is a switcher, so it always lists every context.
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

	// The context the interface is about to open on gets its credentials
	// resolved right here, on a plain terminal prompt, before Bubble Tea
	// takes the screen — see resolveStartupCredentials. A context nobody has
	// asked to see yet is never touched merely because the program started
	// (issue #27): it loads, and resolves its own credentials, only once the
	// operator actually selects it, widens into it, or opens an operation
	// that clearly needs it.
	if startCC := startingContext(cfg, current); startCC != nil {
		if err := resolveStartupCredentials(cmd.Context(), a, startCC); err != nil {
			return err
		}
	}

	// A background load's credential prompt cannot read the password from
	// os.Stdin the way the command line does: Bubble Tea already owns the
	// terminal, and a second reader racing it for the same input is what let
	// a keystroke meant for the password field reach a global shortcut
	// instead (issue #26). The coordinator answers "prompt" references
	// through the interface itself, so it replaces the resolver's usual
	// prompt provider for the lifetime of this run. It only ever gets asked
	// about a context reached after start-up: the starting one primed its
	// answer into the resolver above and so never reaches the coordinator at
	// all.
	coordinator := tui.NewPromptCoordinator()
	a.Resolver().SetProvider(coordinator)

	backend := tui.NewBackend(cfg, a.Resolver(), a.Sessions(), a.ConnectOptions())
	snap, runErr := tui.Run(cmd.Context(), backend, tui.Options{
		Current:     current,
		AllContexts: a.AllContexts,
		Kind:        remembered.Kind,
		Sort:        remembered.Sort,

		RefreshInterval: a.RefreshInterval,
		Credentials:     coordinator,
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

// startingContext returns the context the interface is about to open on, the
// same one tui.Options.Current names — current when it names one, otherwise
// whichever context the Model itself falls back to (the first configured
// one, absent any other choice). It returns nil with nothing configured yet,
// which is the one case start-up has no context to resolve credentials for:
// the interface opens straight into the setup form instead.
func startingContext(cfg *config.Config, current string) *config.Context {
	if current != "" {
		if cc, err := cfg.Context(current); err == nil {
			return cc
		}
	}
	if len(cfg.Contexts) > 0 {
		return cfg.Contexts[0]
	}
	return nil
}

// resolveStartupCredentials asks for the starting context's own password —
// and its proxy's, if the route needs one — on a plain terminal prompt
// before Bubble Tea claims the screen, exactly the way "vsfleet context
// test" already asks. A credential already stored (keyring) resolves here
// with no prompt at all; only a genuinely missing secret costs the operator
// a question, and it is asked once, up front, rather than through an overlay
// opened just to immediately show a password field.
//
// The answer is primed into the resolver (see credentials.Resolver.Prime) so
// that the background load which reaches this same context moments later —
// the first thing Model.Init does — finds it already resolved instead of
// asking a second time.
//
// A failure or cancellation here is returned to the caller as a plain CLI
// error: the interface never opens on a context it could not even get
// credentials for.
func resolveStartupCredentials(ctx context.Context, a *App, cc *config.Context) error {
	ref := cc.Credential.WithDefaultLabel(cc.Name)
	cred, _, err := credentials.Resolve(ctx, a.Resolver(), ref, cc.Name)
	if err != nil {
		return fmt.Errorf("context %q: %w", cc.Name, err)
	}
	a.Resolver().Prime(ref, cred)

	if cc.Transport.Username == "" {
		return nil
	}
	proxyRef := cc.Transport.Credential
	proxyPass, err := transport.ResolveProxyCredential(ctx, cc.Transport, transport.Options{Resolver: a.Resolver()})
	if err != nil {
		return fmt.Errorf("context %q: proxy credential: %w", cc.Name, err)
	}
	// A bare "prompt" reference (no value of its own) is not disambiguated
	// for a proxy credential the way it is for the vCenter's own — see
	// WithDefaultLabel — so priming it here could hand this context's proxy
	// password to a different context whose proxy credential happens to be
	// the same bare reference. Leaving it unprimed only costs one more
	// prompt, on the rare background load that reaches this exact context's
	// proxy again.
	if proxyRef.Scheme == credentials.SchemePrompt && proxyRef.Value == "" {
		return nil
	}
	a.Resolver().Prime(proxyRef, credentials.Credential{Password: proxyPass})
	return nil
}
