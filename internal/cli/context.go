package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/easonliuuuuu/vc-tui/internal/config"
	"github.com/easonliuuuuu/vc-tui/internal/credentials"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

func newContextCommand(a *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "context",
		Aliases: []string{"contexts", "ctx"},
		Short:   "Manage vCenter contexts",
	}
	cmd.AddCommand(
		newContextAddCommand(a),
		newContextListCommand(a),
		newContextUseCommand(a),
		newContextShowCommand(a),
		newContextRemoveCommand(a),
		newContextTestCommand(a),
	)
	return cmd
}

// contextFlags mirror every field of a context so that "context add" works
// unattended in a provisioning script as well as interactively.
type contextFlags struct {
	name          string
	endpoint      string
	username      string
	credential    string
	datacenter    string
	transport     string
	socksAddress  string
	socksUser     string
	remoteDNS     bool
	tlsMode       string
	thumbprint    string
	passwordStdin bool
	skipTest      bool
	force         bool
	setCurrent    bool
}

func (f *contextFlags) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVar(&f.name, "name", "", "context name")
	fl.StringVar(&f.endpoint, "endpoint", "", "vCenter endpoint, e.g. https://vcsa.example.internal")
	fl.StringVar(&f.username, "username", "", "vCenter username, e.g. administrator@vsphere.local")
	fl.StringVar(&f.credential, "credential", "", "credential reference: keyring:<key> or prompt")
	fl.StringVar(&f.datacenter, "datacenter", "", "default datacenter for inventory queries")
	fl.StringVar(&f.transport, "transport", "", "network route: direct or socks5")
	fl.StringVar(&f.socksAddress, "socks-address", "", "SOCKS5 proxy address, host:port")
	fl.StringVar(&f.socksUser, "socks-username", "", "username for a SOCKS5 proxy that requires authentication")
	fl.BoolVar(&f.remoteDNS, "remote-dns", false, "resolve the vCenter hostname at the SOCKS5 proxy")
	fl.StringVar(&f.tlsMode, "tls", "", "certificate policy: system, thumbprint or insecure")
	fl.StringVar(&f.thumbprint, "thumbprint", "", "certificate fingerprint to pin; with --tls thumbprint and no value, the presented certificate is fetched")
	fl.BoolVar(&f.passwordStdin, "password-stdin", false, "read the password from standard input")
	fl.BoolVar(&f.skipTest, "no-test", false, "save without testing the connection")
	fl.BoolVar(&f.force, "force", false, "replace an existing context of the same name")
	fl.BoolVar(&f.setCurrent, "use", false, "make this the current context")
}

func newContextAddCommand(a *App) *cobra.Command {
	var f contextFlags
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a vCenter context",
		Long: `Add a vCenter context.

Run without flags for an interactive wizard. Pass --name, --endpoint and
--username to add a context unattended, which is what a provisioning script
wants. The password is never written to the configuration file: it goes to
the operating system keyring, and the file records only a reference to it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runContextAdd(cmd.Context(), a, &f)
		},
	}
	f.register(cmd)
	return cmd
}

func runContextAdd(ctx context.Context, a *App, f *contextFlags) error {
	cfg, err := a.Config()
	if err != nil {
		return err
	}
	interactive := f.name == "" || f.endpoint == "" || f.username == ""
	p := a.Prompt()
	if interactive && !p.Interactive() {
		return errors.New("context add needs --name, --endpoint and --username when standard input is not a terminal")
	}

	cc := &config.Context{
		Name:       f.name,
		Endpoint:   f.endpoint,
		Username:   f.username,
		Datacenter: f.datacenter,
		Transport: config.TransportConfig{
			Type:      f.transport,
			Address:   f.socksAddress,
			Username:  f.socksUser,
			RemoteDNS: f.remoteDNS,
		},
		TLS: config.TLSConfig{Mode: f.tlsMode, Thumbprint: f.thumbprint},
	}

	var password string
	var havePassword bool
	if f.passwordStdin {
		s, err := p.ReadSecret("")
		if err != nil {
			return err
		}
		password, havePassword = s, true
	}

	if interactive {
		if err := runAddWizard(a, cc, f); err != nil {
			return err
		}
		if !havePassword {
			s, err := p.ReadSecret(fmt.Sprintf("Password for %s: ", cc.Username))
			if err != nil {
				return err
			}
			password, havePassword = s, true
		}
	}

	cc.Normalize()
	if f.credential != "" {
		ref, err := credentials.ParseRef(f.credential)
		if err != nil {
			return err
		}
		cc.Credential = ref
	} else if cc.Credential.IsZero() {
		cc.Credential = credentials.Ref{Scheme: credentials.SchemeKeyring, Value: cc.Name}
	}
	// A pinned context with no fingerprint yet: fetch what the server presents
	// so the operator can look at it before trusting it. This runs before
	// validation, which would otherwise reject the not-yet-known fingerprint.
	if cc.TLS.Mode == config.TLSThumbprint && cc.TLS.Thumbprint == "" {
		if err := discoverThumbprint(ctx, a, cc, interactive); err != nil {
			return err
		}
	}
	if err := cc.Validate(); err != nil {
		return err
	}

	opts := a.ConnectOptions()
	if havePassword {
		opts.Credential = &credentials.Credential{Password: password}
	}

	if !f.skipTest {
		fmt.Fprintf(a.errOut(), "Testing connection to %s ...\n", cc.Endpoint)
		diag, client := vsphere.Diagnose(ctx, cc, opts)
		if client != nil {
			_ = client.Close(context.WithoutCancel(ctx))
		}
		if !diag.OK() {
			printDiagnosis(a, diag, true)
			if !f.force {
				return fmt.Errorf("connection test failed; fix the problem or pass --no-test to save anyway")
			}
			fmt.Fprintln(a.errOut(), "Saving anyway because --force was given.")
		} else {
			fmt.Fprintf(a.errOut(), "%s Connected to %s in %s\n", glyphOK, diag.About.FullVersion(), humanDuration(diag.Latency))
		}
	}

	if havePassword && cc.Credential.Scheme == credentials.SchemeKeyring {
		if err := a.Resolver().Store(ctx, cc.Credential, credentials.Credential{Password: password}); err != nil {
			fmt.Fprintf(a.errOut(), "warning: could not store the password (%v)\n", err)
			fmt.Fprintf(a.errOut(), "vctui will ask for it on each run. Set credential = \"prompt\" to make that explicit.\n")
		}
	}

	if err := cfg.Add(cc, f.force); err != nil {
		return err
	}
	if f.setCurrent || cfg.CurrentContext == "" {
		cfg.CurrentContext = cc.Name
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(a.out(), "Saved context %q to %s\n", cc.Name, cfg.Path())
	return nil
}

// runAddWizard fills in whatever the flags left empty. Editing TOML by hand is
// a fine second step, but it should not be the first thing a new user meets.
func runAddWizard(a *App, cc *config.Context, f *contextFlags) error {
	p := a.Prompt()
	out := a.errOut()
	fmt.Fprintln(out, "Add vCenter")
	fmt.Fprintln(out)

	ask := func(label, current string, required bool) (string, error) {
		for {
			prompt := "  " + label
			if current != "" {
				prompt += " [" + current + "]"
			}
			prompt += ": "
			v, err := p.ReadLine(prompt)
			if err != nil {
				return "", err
			}
			v = strings.TrimSpace(v)
			if v == "" {
				v = current
			}
			if v != "" || !required {
				return v, nil
			}
			fmt.Fprintln(out, "  (required)")
		}
	}
	yes := func(label string, def bool) (bool, error) {
		suffix := " [y/N]: "
		if def {
			suffix = " [Y/n]: "
		}
		v, err := p.ReadLine("  " + label + suffix)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		default:
			return false, nil
		}
	}
	choose := func(label string, options []string, def string) (string, error) {
		for {
			v, err := p.ReadLine(fmt.Sprintf("  %s (%s) [%s]: ", label, strings.Join(options, "/"), def))
			if err != nil {
				return "", err
			}
			v = strings.ToLower(strings.TrimSpace(v))
			if v == "" {
				return def, nil
			}
			for _, o := range options {
				if o == v {
					return v, nil
				}
			}
			fmt.Fprintf(out, "  choose one of: %s\n", strings.Join(options, ", "))
		}
	}

	var err error
	if cc.Name, err = ask("Name", cc.Name, true); err != nil {
		return err
	}
	if cc.Endpoint, err = ask("Endpoint", cc.Endpoint, true); err != nil {
		return err
	}
	if cc.Username, err = ask("Username", firstNonEmpty(cc.Username, "administrator@vsphere.local"), true); err != nil {
		return err
	}
	if f.transport == "" {
		if cc.Transport.Type, err = choose("Connection", []string{config.TransportDirect, config.TransportSOCKS5}, config.TransportDirect); err != nil {
			return err
		}
	}
	if cc.Transport.Type == config.TransportSOCKS5 {
		if cc.Transport.Address, err = ask("SOCKS5 address", firstNonEmpty(cc.Transport.Address, "127.0.0.1:1080"), true); err != nil {
			return err
		}
		if !f.remoteDNS {
			if cc.Transport.RemoteDNS, err = yes("Resolve DNS through the proxy?", true); err != nil {
				return err
			}
		}
	}
	if f.tlsMode == "" {
		if cc.TLS.Mode, err = choose("Certificate policy", []string{config.TLSSystem, config.TLSThumbprint, config.TLSInsecure}, config.TLSSystem); err != nil {
			return err
		}
	}
	if cc.Datacenter, err = ask("Default datacenter (optional)", cc.Datacenter, false); err != nil {
		return err
	}
	fmt.Fprintln(out)
	return nil
}

// discoverThumbprint fetches the certificate a vCenter presents and, when a
// terminal is attached, shows it before pinning. Trust on first use is a
// decision the operator makes, not one the tool makes quietly.
func discoverThumbprint(ctx context.Context, a *App, cc *config.Context, interactive bool) error {
	sha256, sha1, subject, notAfter, err := vsphere.FetchThumbprint(ctx, cc, a.ConnectOptions())
	if err != nil {
		return fmt.Errorf("fetch certificate from %s: %w", cc.Endpoint, err)
	}
	out := a.errOut()
	fmt.Fprintf(out, "Certificate presented by %s\n", cc.Host())
	fl := newFields(out)
	fl.add("Subject", subject)
	fl.add("Expires", notAfter.Format(time.DateOnly))
	fl.add("SHA-256", sha256)
	fl.add("SHA-1", sha1)
	fl.flush()
	if interactive {
		v, err := a.Prompt().ReadLine("  Pin this certificate? [Y/n]: ")
		if err != nil {
			return err
		}
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "", "y", "yes":
		default:
			return errors.New("certificate not pinned")
		}
	}
	cc.TLS.Thumbprint = sha256
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func newContextListCommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List configured contexts",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			if a.json() {
				return writeJSON(a.out(), cfg.Contexts)
			}
			if len(cfg.Contexts) == 0 {
				fmt.Fprintf(a.out(), "No contexts configured. Run \"vctui context add\".\n")
				return nil
			}
			t := newTable(a.out(), "", "NAME", "ENDPOINT", "USERNAME", "ROUTE", "TLS")
			for _, c := range cfg.Contexts {
				marker := " "
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

func newContextUseCommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the current context",
		Args:  cobra.ExactArgs(1),
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
			fmt.Fprintf(a.out(), "Current context is now %q\n", c.Name)
			return nil
		},
	}
}

func newContextShowCommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "show [name]",
		Short: "Show one context in full",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			c, err := a.SingleContext(name)
			if err != nil {
				return err
			}
			if a.json() {
				return writeJSON(a.out(), c)
			}
			fmt.Fprintf(a.out(), "Context: %s\n", c.Name)
			f := newFields(a.out())
			f.add("Endpoint", c.Endpoint)
			f.add("Username", c.Username)
			f.add("Credential", dash(c.Credential.String()))
			f.add("Datacenter", c.Datacenter)
			f.add("Route", c.Transport.Describe())
			f.add("TLS", c.TLS.Describe())
			f.add("Thumbprint", c.TLS.Thumbprint)
			f.flush()
			return nil
		},
	}
}

func newContextRemoveCommand(a *App) *cobra.Command {
	var alsoCredential bool
	cmd := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a context",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			c, err := cfg.Context(args[0])
			if err != nil {
				return err
			}
			ref := c.Credential
			if err := cfg.Remove(c.Name); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			if alsoCredential && ref.Scheme == credentials.SchemeKeyring {
				if err := a.Resolver().Delete(cmd.Context(), ref); err != nil && !errors.Is(err, credentials.ErrNotFound) {
					fmt.Fprintf(a.errOut(), "warning: could not remove %s: %v\n", ref, err)
				}
			}
			fmt.Fprintf(a.out(), "Removed context %q\n", c.Name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&alsoCredential, "with-credential", false, "also delete the stored password from the system keyring")
	return cmd
}

func newContextTestCommand(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "test [name]",
		Short: "Test the connection to a context",
		Long: `Test the connection to a context.

Every stage of the path is checked separately, because "cannot connect" is
not a diagnosis: a dead proxy, an expired password and a rotated certificate
all look the same from the outside.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			c, err := a.SingleContext(name)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			diag, client := vsphere.Diagnose(ctx, c, a.ConnectOptions())
			if client != nil {
				_ = client.Close(context.WithoutCancel(ctx))
			}
			if a.json() {
				if err := writeJSON(a.out(), diagnosisJSON(diag)); err != nil {
					return err
				}
				return diag.Err()
			}
			printContextTest(a, diag)
			return diag.Err()
		},
	}
}

// printContextTest renders the compact summary of a connection test.
func printContextTest(a *App, d *vsphere.Diagnosis) {
	out := a.out()
	fmt.Fprintf(out, "Context: %s\n", d.Context)
	f := newFields(out)
	f.add("Endpoint", d.Endpoint)
	f.add("Route", d.Route)
	f.add("DNS", dnsSummary(d))
	f.add("TLS", tlsSummary(d))
	f.add("Auth", stageSummary(d, "Authentication"))
	if d.About.Version != "" {
		f.add("vCenter", d.About.FullVersion())
	}
	if d.Latency > 0 {
		f.add("Latency", humanDuration(d.Latency))
	}
	f.flush()
	if d.OK() {
		fmt.Fprintln(out, "Connection successful.")
		return
	}
	fmt.Fprintln(out)
	printDiagnosis(a, d, false)
}

func stage(d *vsphere.Diagnosis, name string) *vsphere.Check {
	for i := range d.Checks {
		if d.Checks[i].Name == name {
			return &d.Checks[i]
		}
	}
	return nil
}

func stageSummary(d *vsphere.Diagnosis, name string) string {
	c := stage(d, name)
	switch {
	case c == nil:
		return ""
	case c.Status == vsphere.CheckPass:
		return "OK"
	case c.Status == vsphere.CheckSkip:
		return "not reached"
	default:
		return "failed"
	}
}

func dnsSummary(d *vsphere.Diagnosis) string {
	c := stage(d, "DNS resolution")
	if c == nil {
		return ""
	}
	if c.Status == vsphere.CheckSkip && c.Detail == "resolved at the proxy" {
		return "Remote"
	}
	if c.Status == vsphere.CheckPass {
		return "Local (" + c.Detail + ")"
	}
	return stageSummary(d, "DNS resolution")
}

func tlsSummary(d *vsphere.Diagnosis) string {
	c := stage(d, "TLS certificate")
	switch {
	case c == nil:
		return ""
	case c.Status == vsphere.CheckPass:
		return "Verified (" + d.TLS + ")"
	case c.Status == vsphere.CheckSkip:
		return c.Detail
	default:
		return "failed"
	}
}
