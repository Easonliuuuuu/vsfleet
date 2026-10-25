package cli

import (
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/contextops"
)

// newConfigCommand groups the context verbs under kubectl's names. A context
// is vsfleet's namespace: -n picks one, -A spans them all, and these verbs
// manage the list.
func newConfigCommand(a *App) *cobra.Command {
	cmd := requireSubcommand(&cobra.Command{
		Use: "config",
		// ctx is kubectx muscle memory; aliases stay out of the root listing.
		Aliases: []string{"ctx"},
		Short:   "Manage vCenter contexts",
		Long: strings.TrimSpace(`
A context names one vCenter and everything needed to reach it: endpoint,
credential reference, network route and TLS policy. Contexts are independent,
so a lab reached directly and a customer vCenter reached through a SOCKS5
proxy work side by side.

A context is vsfleet's namespace. Every command acts on the current context
unless -n (--context) names others or -A (--all-contexts) selects them all.

Passwords are never written to the configuration file. The file records a
reference; the secret lives in the OS keyring or comes from a prompt, an
environment variable, a file or a program at the moment it is needed.`),
		Example: `  # Add a context interactively
  vsfleet config set-context

  # Add one unattended, from a provisioning script
  vsfleet config set-context prod \
    --endpoint https://vcsa.example.internal \
    --username svc-ro@vsphere.local --password-stdin < secret.txt

  # See what is configured and which one is current
  vsfleet config get-contexts

  # Switch the current context, then confirm it works
  vsfleet config use-context prod
  vsfleet doctor`,
	})
	cmd.AddCommand(
		newSetContextCommand(a),
		newGetContextsCommand(a),
		newCurrentContextCommand(a),
		newUseContextCommand(a),
		newDeleteContextCommand(a),
		newConfigViewCommand(a),
	)
	return cmd
}

func newGetContextsCommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "get-contexts [name...]",
		Short: "List configured contexts",
		Example: `  # Every context, with the current one marked
  vsfleet config get-contexts

  # Only some, as JSON
  vsfleet config get-contexts prod dr -o json`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			contexts := cfg.Contexts
			if len(args) > 0 {
				if contexts, err = cfg.Resolve(args, false); err != nil {
					return err
				}
			}
			if a.json() {
				return writeJSON(a.out(), contexts)
			}
			if len(contexts) == 0 {
				fmt.Fprintf(a.out(), "No contexts configured. Run \"vsfleet config set-context\".\n")
				return nil
			}
			t := newTable(a.out(), "CURRENT", "NAME", "ENDPOINT", "USERNAME", "ROUTE", "TLS")
			for _, c := range contexts {
				marker := ""
				if c.Name == cfg.CurrentContext {
					marker = "*"
				}
				t.row(marker, c.Name, c.Endpoint, c.Username, c.Transport.Describe(), c.TLS.Mode)
			}
			t.flush()
			return nil
		},
	}
}

func newCurrentContextCommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "current-context",
		Short: "Print the current context",
		Example: `  # Which vCenter commands act on by default
  vsfleet config current-context`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			c, err := cfg.Context("")
			if err != nil {
				return err
			}
			if a.json() {
				return writeJSON(a.out(), c.Name)
			}
			fmt.Fprintln(a.out(), c.Name)
			return nil
		},
	}
}

func newUseContextCommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "use-context <name>",
		Short: "Set the current context",
		Example: `  # Make prod the current context
  vsfleet config use-context prod`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			c, err := cfg.Context(args[0])
			if err != nil {
				return err
			}
			cfg.CurrentContext = c.Name
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(a.out(), "Switched to context %q.\n", c.Name)
			return nil
		},
	}
}

func newDeleteContextCommand(a *App) *cobra.Command {
	var alsoCredential bool
	cmd := &cobra.Command{
		Use:   "delete-context <name>",
		Short: "Delete a context",
		Example: `  # Delete a context
  vsfleet config delete-context lab

  # Delete it and the password stored for it in the OS keyring
  vsfleet config delete-context lab --with-credential`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			wasCurrent := cfg.CurrentContext == args[0]
			c, err := contextops.Remove(cfg, args[0])
			if err != nil {
				return err
			}
			if alsoCredential {
				if err := contextops.DeleteCredentials(cmd.Context(), a.Resolver(), c); err != nil {
					fmt.Fprintf(a.errOut(), "warning: could not remove stored password(s): %v\n", err)
				}
			}
			if wasCurrent {
				fmt.Fprintf(a.errOut(), "warning: this removed your current context; use \"vsfleet config use-context\" to select another\n")
			}
			fmt.Fprintf(a.out(), "Deleted context %q from %s\n", c.Name, cfg.Path())
			return nil
		},
	}
	cmd.Flags().BoolVar(&alsoCredential, "with-credential", false, "also delete the stored password from the system keyring")
	return cmd
}

// configView is the JSON shape of "config view". config.Config carries only
// TOML tags, and its field names are not what a JSON reader expects.
type configView struct {
	Version        int               `json:"version"`
	CurrentContext string            `json:"current_context,omitempty"`
	Contexts       []*config.Context `json:"contexts"`
	SSH            *config.SSHConfig `json:"ssh,omitempty"`
}

func newConfigViewCommand(a *App) *cobra.Command {
	var minify bool
	cmd := &cobra.Command{
		Use:   "view",
		Short: "Show the configuration",
		Example: `  # The whole configuration, as it would be written to the file
  vsfleet config view

  # Only the current context
  vsfleet config view --minify

  # One named context, as JSON
  vsfleet config view --minify -n prod -o json`,
		Long: `Show the configuration.

The output is what the configuration file holds, normalized. Credentials
appear as references such as keyring:prod or env:VSPHERE_PASSWORD; the file
never holds a password, so neither does this.

--minify keeps only the context a command would act on: the current one, or
the one named with -n.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			view := &config.Config{Version: cfg.Version, CurrentContext: cfg.CurrentContext, Contexts: cfg.Contexts, SSH: cfg.SSH}
			if minify {
				c, err := a.SingleContext("")
				if err != nil {
					return err
				}
				view = &config.Config{Version: cfg.Version, CurrentContext: c.Name, Contexts: []*config.Context{c}}
			}
			if view.Contexts == nil {
				view.Contexts = []*config.Context{}
			}
			if a.json() {
				v := configView{Version: view.Version, CurrentContext: view.CurrentContext, Contexts: view.Contexts}
				if !isZeroSSH(view.SSH) {
					v.SSH = &view.SSH
				}
				return writeJSON(a.out(), v)
			}
			b, err := toml.Marshal(view)
			if err != nil {
				return fmt.Errorf("encode configuration: %w", err)
			}
			_, err = a.out().Write(b)
			return err
		},
	}
	cmd.Flags().BoolVar(&minify, "minify", false, "show only the selected context")
	return cmd
}

func isZeroSSH(s config.SSHConfig) bool {
	return s.User == "" && s.VMUser == "" && s.HostUser == "" && len(s.Routes) == 0
}

// contextRenames maps each removed "context" subcommand to its new form.
var contextRenames = map[string]string{
	"add":    "vsfleet config set-context NAME",
	"list":   "vsfleet config get-contexts",
	"ls":     "vsfleet config get-contexts",
	"use":    "vsfleet config use-context NAME",
	"show":   "vsfleet config view --minify -n NAME",
	"remove": "vsfleet config delete-context NAME",
	"rm":     "vsfleet config delete-context NAME",
	"delete": "vsfleet config delete-context NAME",
	"test":   "vsfleet doctor NAME",
}

// newContextRenamedCommand answers the removed "context" noun with the
// command that replaced the subcommand typed, rather than one long list.
func newContextRenamedCommand() *cobra.Command {
	cmd := renamedCommand("context", []string{"contexts"},
		`use "vsfleet config get-contexts|current-context|use-context|set-context|delete-context|view", or "vsfleet doctor" to test a connection`)
	generic := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if len(args) > 0 {
			if to, ok := contextRenames[args[0]]; ok {
				return fmt.Errorf("\"context %s\" was renamed: use %q", args[0], to)
			}
		}
		return generic(c, args)
	}
	return cmd
}
