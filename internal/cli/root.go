// Package cli implements the vsfleet command line. Every command is a thin
// wrapper over the configuration, session and inventory layers, which is what
// lets the terminal interface be a second front end rather than a second
// implementation.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/credentials"
	"github.com/easonliuuuuu/vsfleet/internal/session"
	"github.com/easonliuuuuu/vsfleet/internal/version"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// App holds everything the commands share: where the configuration lives, how
// to resolve credentials, and the sessions opened so far.
type App struct {
	ConfigPath   string
	ContextNames []string
	AllContexts  bool
	Format       string
	Timeout      time.Duration
	HistoryPath  string
	// RefreshInterval is how often the terminal interface re-reads inventory
	// in the background. Zero takes the built-in default; negative turns it
	// off. It only reaches the interface, so it is registered on the two
	// commands that open one rather than on every command in the tree.
	RefreshInterval time.Duration

	In  io.Reader
	Out io.Writer
	Err io.Writer

	cfg       *config.Config
	resolver  *credentials.Resolver
	prompt    *credentials.Prompt
	mgr       *session.Manager
	history   *assessment.Store
	collector *assessment.Collector
}

// Config loads the configuration once per process.
func (a *App) Config() (*config.Config, error) {
	if a.cfg != nil {
		return a.cfg, nil
	}
	cfg, err := config.Load(a.ConfigPath)
	if err != nil {
		return nil, err
	}
	a.cfg = cfg
	return cfg, nil
}

// Prompt returns the interactive credential prompt.
func (a *App) Prompt() *credentials.Prompt {
	if a.prompt == nil {
		a.prompt = &credentials.Prompt{In: a.in(), Out: a.errOut()}
	}
	return a.prompt
}

// Resolver returns the credential resolver: OS keyring first, falling back to
// an interactive prompt when nothing is stored.
func (a *App) Resolver() *credentials.Resolver {
	if a.resolver == nil {
		a.resolver = credentials.NewResolver(credentials.NewKeyring(), a.Prompt())
	}
	return a.resolver
}

// Sessions returns the session manager.
func (a *App) Sessions() *session.Manager {
	if a.mgr == nil {
		m := session.New(a.Resolver())
		m.ConnectOptions.UserAgent = version.UserAgent()
		if a.Timeout > 0 {
			m.ConnectTimeout = a.Timeout
			m.ConnectOptions.DialTimeout = a.Timeout
		}
		a.mgr = m
	}
	return a.mgr
}

// ConnectOptions builds the options used for one-off connections.
func (a *App) ConnectOptions() vsphere.ConnectOptions {
	return vsphere.ConnectOptions{
		Resolver:    a.Resolver(),
		DialTimeout: a.Timeout,
		UserAgent:   version.UserAgent(),
	}
}

// History opens the local assessment database once for the process. Callers
// may treat an open failure as a feature warning: live inventory does not
// depend on historical storage being available.
func (a *App) History() (*assessment.Store, error) {
	if a.history != nil {
		return a.history, nil
	}
	s, err := assessment.Open(a.HistoryPath)
	if err != nil {
		return nil, err
	}
	a.history = s
	return s, nil
}

func (a *App) Collector() (*assessment.Collector, error) {
	if a.collector != nil {
		return a.collector, nil
	}
	s, err := a.History()
	if err != nil {
		return nil, err
	}
	a.collector = &assessment.Collector{Store: s, Manager: a.Sessions()}
	return a.collector, nil
}

func (a *App) Assessment() (*assessment.Service, error) {
	c, err := a.Collector()
	if err != nil {
		return nil, err
	}
	return &assessment.Service{Store: a.history, Collector: c}, nil
}

// Contexts resolves the contexts a command should act on, honouring --context
// and --all-contexts.
func (a *App) Contexts() ([]*config.Context, error) {
	cfg, err := a.Config()
	if err != nil {
		return nil, err
	}
	return cfg.Resolve(a.ContextNames, a.AllContexts)
}

// SingleContext resolves exactly one context, for commands that cannot span
// several vCenters.
func (a *App) SingleContext(name string) (*config.Context, error) {
	cfg, err := a.Config()
	if err != nil {
		return nil, err
	}
	if name == "" && len(a.ContextNames) == 1 {
		name = a.ContextNames[0]
	}
	return cfg.Context(name)
}

func (a *App) in() io.Reader {
	if a.In != nil {
		return a.In
	}
	return os.Stdin
}

func (a *App) out() io.Writer {
	if a.Out != nil {
		return a.Out
	}
	return os.Stdout
}

func (a *App) errOut() io.Writer {
	if a.Err != nil {
		return a.Err
	}
	return os.Stderr
}

func (a *App) json() bool { return a.Format == FormatJSON }

func (a *App) checkFormat() error {
	switch a.Format {
	case FormatTable, FormatJSON, "":
		return nil
	default:
		return fmt.Errorf("unknown output format %q (supported: table, json)", a.Format)
	}
}

// NewRootCommand builds the vsfleet command tree.
func NewRootCommand(a *App) *cobra.Command {
	root := &cobra.Command{
		Use:   "vsfleet",
		Short: "Operate all your vCenters from one terminal",
		Long: `vsfleet treats every vCenter as a named context, the way kubectl treats
clusters. Each context carries its own endpoint, credential reference,
network route and TLS policy, so a lab reached directly and a customer
vCenter reached through a SOCKS5 proxy work side by side in one process.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.String(),
		// A bare "vsfleet" opens the terminal interface: that is the product,
		// not the subcommand tree. Args stays NoArgs so a genuine typo like
		// "vsfleet statas" still reports "unknown command" instead of quietly
		// launching the interface with an argument it ignores.
		Args: cobra.NoArgs,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return a.checkFormat()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUI(a, cmd)
		},
	}
	root.SetOut(a.out())
	root.SetErr(a.errOut())
	root.SetIn(a.in())

	f := root.PersistentFlags()
	f.StringVar(&a.ConfigPath, "config", "", "path to the configuration file (default: platform user config directory)")
	f.StringSliceVar(&a.ContextNames, "context", nil, "context to act on, repeatable (default: the current context)")
	f.BoolVar(&a.AllContexts, "all-contexts", false, "act on every configured context")
	f.StringVarP(&a.Format, "output", "o", FormatTable, "output format: table or json")
	f.DurationVar(&a.Timeout, "timeout", 30*time.Second, "per-vCenter timeout")
	f.StringVar(&a.HistoryPath, "history-db", "", "path to the local assessment database")
	// A bare "vsfleet" opens the interface, so the interface's own flag
	// belongs here too — on root's own flags, not the persistent set, so
	// "vsfleet vm list --refresh" is still the error it should be.
	addRefreshFlag(root, a)

	root.AddCommand(
		newContextCommand(a),
		newDemoCommand(a),
		newDoctorCommand(a),
		newSearchCommand(a),
		newStatusCommand(a),
		newUICommand(a),
		newAssessmentCommand(a),
		newHealthCommand(a),
	)
	root.AddCommand(newInventoryCommands(a)...)
	return root
}

// Execute runs the command tree and reports failures on stderr.
func Execute(ctx context.Context) int {
	a := &App{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
	root := NewRootCommand(a)
	defer func() {
		if a.mgr != nil {
			_ = a.mgr.Close(context.WithoutCancel(ctx))
		}
		if a.history != nil {
			_ = a.history.Close()
		}
	}()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(a.errOut(), "vsfleet: %v\n", err)
		var coded interface{ ExitCode() int }
		if errors.As(err, &coded) {
			return coded.ExitCode()
		}
		return 1
	}
	return 0
}
