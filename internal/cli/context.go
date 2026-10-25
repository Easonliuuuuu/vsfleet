package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/contextops"
	"github.com/easonliuuuuu/vsfleet/internal/credentials"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// contextFlags mirror every field of a context so that "config set-context"
// works unattended in a provisioning script as well as interactively.
type contextFlags struct {
	name               string
	endpoint           string
	username           string
	via                string
	viaMoRef           string
	credential         string
	datacenter         string
	transport          string
	proxyAddress       string
	proxyUsername      string
	proxyCredential    string
	remoteDNS          bool
	tlsMode            string
	thumbprint         string
	passwordStdin      bool
	proxyPasswordStdin bool
	skipTest           bool
	force              bool
	setCurrent         bool
	// editing is set when the named context already exists, so the wizard
	// offers its current values and a blank password keeps the stored one.
	editing bool
}

// contextBehaviourFlags change how set-context runs, not what it saves, so
// passing only these to an existing context leaves nothing to change.
var contextBehaviourFlags = map[string]bool{"no-test": true, "force": true}

func (f *contextFlags) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVar(&f.endpoint, "endpoint", "", "vCenter endpoint, e.g. https://vcsa.example.internal")
	fl.StringVar(&f.username, "username", "", "vCenter username, e.g. administrator@vsphere.local")
	fl.StringVar(&f.via, "via", "", "parent context this was added from")
	fl.StringVar(&f.viaMoRef, "via-moref", "", "managed object reference of the VM this was added from")
	fl.StringVar(&f.credential, "credential", "", "credential reference: keyring:<key>, prompt, env:<VAR>, file:<path> or exec:<program>")
	fl.StringVar(&f.datacenter, "datacenter", "", "default datacenter for inventory queries")
	fl.StringVar(&f.transport, "transport", "", "network route: direct, socks5, http or https")
	fl.StringVar(&f.proxyAddress, "proxy-address", "", "proxy address for socks5/http/https, host:port")
	fl.StringVar(&f.proxyUsername, "proxy-username", "", "username for a proxy that requires authentication")
	fl.StringVar(&f.proxyCredential, "proxy-credential", "", "credential reference for the proxy password: keyring:<key>, env:<VAR>, file:<path> or exec:<program> (default keyring:<name>-proxy)")
	fl.BoolVar(&f.remoteDNS, "remote-dns", false, "resolve the vCenter hostname at the proxy (socks5 only; http and https always do)")
	fl.StringVar(&f.tlsMode, "tls", "", "certificate policy: system, thumbprint or insecure")
	fl.StringVar(&f.thumbprint, "thumbprint", "", "certificate fingerprint to pin; with --tls thumbprint and no value, the presented certificate is fetched")
	fl.BoolVar(&f.passwordStdin, "password-stdin", false, "read the password from standard input")
	fl.BoolVar(&f.proxyPasswordStdin, "proxy-password-stdin", false, "read the proxy password from standard input, after the vCenter password if both are piped in")
	fl.BoolVar(&f.skipTest, "no-test", false, "save without testing the connection")
	fl.BoolVar(&f.force, "force", false, "save even if the connection test fails")
	fl.BoolVar(&f.setCurrent, "use", false, "make this the current context")
}

// apply copies the flags that were passed onto an existing context. A flag
// left out keeps the saved value, so changing one setting does not mean
// restating the rest. Switching the route or the certificate policy starts
// that section clean: a proxy address or a pinned fingerprint means nothing
// under a different one.
func (f *contextFlags) apply(cc *config.Context, fl *pflag.FlagSet) {
	set := func(name string, dst *string, v string) {
		if fl.Changed(name) {
			*dst = v
		}
	}
	set("endpoint", &cc.Endpoint, f.endpoint)
	set("username", &cc.Username, f.username)
	set("via", &cc.Via, f.via)
	set("via-moref", &cc.ViaMoRef, f.viaMoRef)
	set("datacenter", &cc.Datacenter, f.datacenter)
	if fl.Changed("transport") && f.transport != cc.Transport.Type {
		cc.Transport = config.TransportConfig{Type: f.transport}
	}
	set("proxy-address", &cc.Transport.Address, f.proxyAddress)
	set("proxy-username", &cc.Transport.Username, f.proxyUsername)
	if fl.Changed("remote-dns") {
		cc.Transport.RemoteDNS = f.remoteDNS
	}
	if fl.Changed("tls") && f.tlsMode != cc.TLS.Mode {
		cc.TLS = config.TLSConfig{Mode: f.tlsMode}
	}
	set("thumbprint", &cc.TLS.Thumbprint, f.thumbprint)
}

// changesSettings reports whether any flag that alters the saved context was
// passed. fl holds the command's own flags; --config and the other global
// flags never change a context.
func changesSettings(fl *pflag.FlagSet) bool {
	changed := false
	fl.VisitAll(func(fl *pflag.Flag) {
		if fl.Changed && !contextBehaviourFlags[fl.Name] {
			changed = true
		}
	})
	return changed
}

func newSetContextCommand(a *App) *cobra.Command {
	var f contextFlags
	cmd := &cobra.Command{
		Use:   "set-context [name]",
		Short: "Add a vCenter context or change one",
		Example: `  # Interactive wizard
  vsfleet config set-context

  # Unattended, password piped in and stored in the OS keyring
  vsfleet config set-context prod \
    --endpoint https://vcsa.example.internal \
    --username svc-ro@vsphere.local --password-stdin < secret.txt

  # Change one setting of an existing context; the rest is kept
  vsfleet config set-context prod --datacenter DC1

  # Rotate the stored password
  vsfleet config set-context prod --password-stdin < secret.txt

  # Through a SOCKS5 proxy, resolving DNS at the proxy
  vsfleet config set-context customer-a \
    --endpoint https://vcsa.internal --username svc-ro@vsphere.local \
    --transport socks5 --proxy-address bastion.example.net:1080 --remote-dns

  # Pin the presented certificate instead of trusting the system store
  vsfleet config set-context lab --endpoint https://vcsa.lab \
    --username administrator@vsphere.local --tls thumbprint

  # Take the password from a secret manager at connect time
  vsfleet config set-context ci --endpoint https://vcsa.example.internal \
    --username svc-ro@vsphere.local --credential "exec:vault-read vsphere/ci"`,
		Long: `Add a vCenter context, or change one that exists.

A new context needs a name, --endpoint and --username; on a terminal, the
wizard asks for whatever is missing. For an existing context, only the flags
passed change and everything else is kept. Run it on a terminal with no flags
to walk the wizard with the current values filled in.

The connection is tested before anything is saved. The password is never
written to the configuration file: it goes to the operating system keyring,
and the file records only a reference to it.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				f.name = args[0]
			}
			return runSetContext(cmd.Context(), a, &f, cmd.LocalFlags())
		},
	}
	f.register(cmd)
	return cmd
}

func runSetContext(ctx context.Context, a *App, f *contextFlags, fl *pflag.FlagSet) error {
	cfg, err := a.Config()
	if err != nil {
		return err
	}
	var existing *config.Context
	if f.name != "" {
		if c, err := cfg.Context(f.name); err == nil {
			existing = c
		}
	}
	f.editing = existing != nil

	var cc *config.Context
	if existing != nil {
		c := *existing
		cc = &c
		f.apply(cc, fl)
	} else {
		cc = &config.Context{
			Name:       f.name,
			Endpoint:   f.endpoint,
			Username:   f.username,
			Via:        f.via,
			ViaMoRef:   f.viaMoRef,
			Datacenter: f.datacenter,
			Transport: config.TransportConfig{
				Type:      f.transport,
				Address:   f.proxyAddress,
				Username:  f.proxyUsername,
				RemoteDNS: f.remoteDNS,
			},
			TLS: config.TLSConfig{Mode: f.tlsMode, Thumbprint: f.thumbprint},
		}
	}

	interactive := cc.Name == "" || cc.Endpoint == "" || cc.Username == "" ||
		(existing != nil && !changesSettings(fl))
	p := a.Prompt()
	if interactive && !p.Interactive() {
		if existing != nil {
			return fmt.Errorf("set-context %s: nothing to change; pass the settings to change, or run it on a terminal for the wizard", existing.Name)
		}
		return errors.New("set-context needs a name, --endpoint and --username when standard input is not a terminal")
	}

	// The credential references are parsed before any password is read, so a
	// reference that cannot hold a password rejects the request without first
	// consuming the operator's standard input. An existing context keeps the
	// references it has unless new ones are passed.
	credRef := cc.Credential
	if f.credential != "" {
		ref, err := credentials.ParseRef(f.credential)
		if err != nil {
			return err
		}
		credRef = ref
	}
	proxyCredRef := cc.Transport.Credential
	if f.proxyCredential != "" {
		ref, err := credentials.ParseRef(f.proxyCredential)
		if err != nil {
			return err
		}
		proxyCredRef = ref
	}
	// env, file and exec name where the password lives; they do not store one.
	// Reading a password here and then dropping it silently — which is what
	// contextops.Save does for any non-keyring scheme — would leave an
	// operator believing a secret was saved that never was.
	if credRef.NonInteractive() && f.passwordStdin {
		return fmt.Errorf("--password-stdin cannot be combined with --credential %s: %s resolves the password when it is needed, so there is nothing to store", credRef, credRef.Scheme)
	}
	if proxyCredRef.NonInteractive() && f.proxyPasswordStdin {
		return fmt.Errorf("--proxy-password-stdin cannot be combined with --proxy-credential %s: %s resolves the password when it is needed, so there is nothing to store", proxyCredRef, proxyCredRef.Scheme)
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
	var proxyPassword string
	var haveProxyPassword bool
	if f.proxyPasswordStdin {
		s, err := p.ReadSecret("")
		if err != nil {
			return err
		}
		proxyPassword, haveProxyPassword = s, true
	}

	if interactive {
		if err := runAddWizard(ctx, a, cc, f); err != nil {
			return err
		}
		if f.credential == "" {
			credRef = cc.Credential
		}
		// env, file and exec resolve the password themselves, and the
		// connection test below goes through them — which is the point: a
		// missing variable or a failing helper shows up now, not on the first
		// unattended run. A password typed here would be tested instead and
		// then thrown away.
		if !havePassword && !credRef.NonInteractive() {
			s, err := readWizardSecret(p, fmt.Sprintf("Password for %s", cc.Username), existing != nil)
			if err != nil {
				return err
			}
			password, havePassword = s, s != "" || existing == nil
		}
		if cc.Transport.Username != "" && !haveProxyPassword {
			keep := existing != nil && existing.Transport.Username != ""
			s, err := readWizardSecret(p, fmt.Sprintf("Password for proxy user %s", cc.Transport.Username), keep)
			if err != nil {
				return err
			}
			proxyPassword, haveProxyPassword = s, s != "" || !keep
		}
	}

	// A pinned context with no fingerprint yet: fetch what the server presents
	// so the operator can look at it before trusting it. This runs before
	// validation, which would otherwise reject the not-yet-known fingerprint.
	if cc.TLS.Mode == config.TLSThumbprint && cc.TLS.Thumbprint == "" {
		if err := discoverThumbprint(ctx, a, cc, interactive); err != nil {
			return err
		}
	}

	in := contextops.Input{
		Name: cc.Name, Endpoint: cc.Endpoint, Username: cc.Username, Via: cc.Via, ViaMoRef: cc.ViaMoRef, Datacenter: cc.Datacenter,
		Transport: cc.Transport, TLS: cc.TLS, Credential: credRef,
		Password: password, HavePassword: havePassword,
		ProxyCredential: proxyCredRef, ProxyPassword: proxyPassword, HaveProxyPassword: haveProxyPassword,
		Replace: existing != nil, SaveOnTestFailure: f.force, SetCurrent: f.setCurrent,
		// Moving a context to another password source is an explicit choice,
		// so the keyring entry it leaves behind goes with it.
		DropReplacedKeyring: existing != nil && f.credential != "",
	}

	if !f.skipTest {
		fmt.Fprintf(a.errOut(), "Testing connection to %s ...\n", cc.Endpoint)
	}
	res, err := contextops.Save(ctx, cfg, a.Resolver(), a.ConnectOptions(), in, !f.skipTest)
	if !f.skipTest && res.Diagnosis != nil {
		if res.Diagnosis.OK() {
			fmt.Fprintf(a.errOut(), "%s Connected to %s in %s\n", glyphOK, res.Diagnosis.About.FullVersion(), humanDuration(res.Diagnosis.Latency))
		} else {
			printDiagnosis(a, res.Diagnosis, true)
			if f.force {
				fmt.Fprintln(a.errOut(), "Saving anyway because --force was given.")
			}
		}
	}
	if err != nil {
		if res != nil && res.Diagnosis != nil && !res.Diagnosis.OK() {
			return fmt.Errorf("connection test failed; fix the problem, or pass --force to save anyway or --no-test to skip the test")
		}
		return err
	}
	if res.StoreWarning != nil {
		fmt.Fprintf(a.errOut(), "warning: %v\n", res.StoreWarning)
		fmt.Fprintf(a.errOut(), "vsfleet will ask for it on each run. To run unattended instead, run \"vsfleet config set-context %s\" with\n--credential env:<VAR>, file:<path> or exec:<program>.\n", res.Context.Name)
	}
	if res.DropWarning != nil {
		fmt.Fprintf(a.errOut(), "warning: %v\n", res.DropWarning)
	} else if !res.Dropped.IsZero() {
		fmt.Fprintf(a.errOut(), "Removed the old password %s from the keyring.\n", res.Dropped)
	}
	verb := "created"
	if existing != nil {
		verb = "modified"
	}
	fmt.Fprintf(a.out(), "Context %q %s in %s\n", res.Context.Name, verb, cfg.Path())
	return nil
}

// readWizardSecret asks for a password. When keep is set a stored one already
// exists, and a blank answer keeps it.
func readWizardSecret(p *credentials.Prompt, label string, keep bool) (string, error) {
	if keep {
		label += " (Enter keeps the current one)"
	}
	return p.ReadSecret(label + ": ")
}

// runAddWizard fills in whatever the flags left empty. Editing TOML by hand is
// a fine second step, but it should not be the first thing a new user meets.
func runAddWizard(ctx context.Context, a *App, cc *config.Context, f *contextFlags) error {
	p := a.Prompt()
	out := a.errOut()
	if f.editing {
		fmt.Fprintf(out, "Edit vCenter %s\n", cc.Name)
	} else {
		fmt.Fprintln(out, "Add vCenter")
	}
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
	// A name given on the command line is not asked again; an existing
	// context cannot be renamed here.
	if cc.Name == "" {
		if cc.Name, err = ask("Name", cc.Name, true); err != nil {
			return err
		}
	}
	if cc.Endpoint, err = ask("Endpoint", cc.Endpoint, true); err != nil {
		return err
	}
	if cc.Username, err = ask("Username", firstNonEmpty(cc.Username, "administrator@vsphere.local"), true); err != nil {
		return err
	}
	// A new context's password goes to the OS keyring by default. Where there
	// is none, find out now and let the operator choose where it comes from,
	// rather than taking the password, failing to store it, and downgrading
	// to a prompt on every run behind a warning. A password already piped in
	// with --password-stdin is a request for the keyring; leave it to Save.
	// An existing context keeps the source it has.
	if f.credential == "" && !f.passwordStdin && cc.Credential.IsZero() {
		if perr := a.Resolver().CheckAvailable(ctx, credentials.SchemeKeyring); perr != nil {
			if cc.Credential, err = chooseCredentialSource(ask, choose, out, perr); err != nil {
				return err
			}
		}
	}
	if f.transport == "" {
		if cc.Transport.Type, err = choose("Connection", []string{
			config.TransportDirect, config.TransportSOCKS5, config.TransportHTTPProxy, config.TransportHTTPSProxy,
		}, firstNonEmpty(cc.Transport.Type, config.TransportDirect)); err != nil {
			return err
		}
	}
	if cc.Transport.Type == config.TransportDirect {
		// An edit that drops the proxy drops its settings with it.
		cc.Transport = config.TransportConfig{Type: config.TransportDirect}
	} else {
		if cc.Transport.Address, err = ask("Proxy address", firstNonEmpty(cc.Transport.Address, "127.0.0.1:1080"), true); err != nil {
			return err
		}
		if cc.Transport.Type == config.TransportSOCKS5 && !f.remoteDNS {
			if cc.Transport.RemoteDNS, err = yes("Resolve DNS through the proxy?", !f.editing || cc.Transport.RemoteDNS); err != nil {
				return err
			}
		}
		if cc.Transport.Username, err = ask("Proxy username (optional)", cc.Transport.Username, false); err != nil {
			return err
		}
	}
	if f.tlsMode == "" {
		if cc.TLS.Mode, err = choose("Certificate policy", []string{config.TLSSystem, config.TLSThumbprint, config.TLSInsecure}, firstNonEmpty(cc.TLS.Mode, config.TLSSystem)); err != nil {
			return err
		}
	}
	if cc.Datacenter, err = ask("Default datacenter (optional)", cc.Datacenter, false); err != nil {
		return err
	}
	fmt.Fprintln(out)
	return nil
}

// chooseCredentialSource asks where a password comes from when the OS keyring
// cannot hold it. Every source that works without one is offered, and for
// env, file and exec only where the password lives is collected — never the
// password itself, which none of them stores. ask and choose are the wizard's
// own question helpers, so this step reads like the rest of it.
func chooseCredentialSource(
	ask func(label, current string, required bool) (string, error),
	choose func(label string, options []string, def string) (string, error),
	out io.Writer,
	reason error,
) (credentials.Ref, error) {
	fmt.Fprintf(out, "  No OS keyring is available here, so the password cannot be stored.\n")
	fmt.Fprintf(out, "  (%v)\n", reason)
	fmt.Fprintln(out, "  Where should the password come from?")
	fmt.Fprintln(out, "    prompt  ask on every run; nothing is stored")
	fmt.Fprintln(out, "    env     an environment variable")
	fmt.Fprintln(out, "    file    a file you protect, such as a mounted secret")
	fmt.Fprintln(out, "    exec    a helper program that prints it (vault, pass, op, ...)")
	scheme, err := choose("Password source", []string{
		credentials.SchemePrompt, credentials.SchemeEnv, credentials.SchemeFile, credentials.SchemeExec,
	}, credentials.SchemePrompt)
	if err != nil {
		return credentials.Ref{}, err
	}
	if scheme == credentials.SchemePrompt {
		return credentials.Ref{Scheme: credentials.SchemePrompt}, nil
	}
	label := map[string]string{
		credentials.SchemeEnv:  "Environment variable",
		credentials.SchemeFile: "Password file path",
		credentials.SchemeExec: "Helper program path",
	}[scheme]
	for {
		v, err := ask(label, "", true)
		if err != nil {
			return credentials.Ref{}, err
		}
		ref, err := credentials.ParseRef(scheme + ":" + v)
		if err != nil {
			fmt.Fprintf(out, "  %v\n", err)
			continue
		}
		return ref, nil
	}
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
