package credentials

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Environment variables every exec: helper is given, so one wrapper script can
// serve several contexts rather than needing one program per vCenter.
const (
	EnvHelperContext = "VSFLEET_CONTEXT"
	EnvHelperRef     = "VSFLEET_CREDENTIAL_REF"
)

// maxSecretBytes bounds what a file: or exec: source may hand back. A password
// is a line, not a stream; without a ceiling a mistaken reference at a device
// or a helper that never stops writing would be read until the process ran out
// of memory.
const maxSecretBytes = 64 << 10

// helperWaitDelay is how long an exec: helper gets to release its output pipes
// after its context has been cancelled and it has been killed.
const helperWaitDelay = 2 * time.Second

// trimSecret removes the single trailing newline that every ordinary way of
// producing a secret adds — a here-doc, `echo`, a text editor saving a file,
// `vault read` writing to stdout. Only one is removed, and no other whitespace
// is touched: a password may legitimately end in a space, and silently
// trimming it would turn a working credential into an authentication failure
// nobody can explain.
func trimSecret(b []byte) string {
	s := string(b)
	s = strings.TrimSuffix(s, "\n")
	return strings.TrimSuffix(s, "\r")
}

// Env reads a secret from an environment variable named by the reference, as
// in "env:VSFLEET_PROD_PASSWORD". The reference names the variable; it never
// contains the secret, so it stays safe to write in config.toml and to print.
//
// It suits CI systems and container runtimes that inject secrets into the
// process environment. On a shared host the environment of a running process
// is readable through /proc to the same user, so file: is the better choice
// where that matters.
type Env struct {
	// LookupEnv overrides the process environment. Tests set this.
	LookupEnv func(string) (string, bool)
}

// NewEnv returns an Env provider reading the process environment.
func NewEnv() *Env { return &Env{} }

func (e *Env) Scheme() string { return SchemeEnv }

func (e *Env) lookup(name string) (string, bool) {
	if e.LookupEnv != nil {
		return e.LookupEnv(name)
	}
	return os.LookupEnv(name)
}

func (e *Env) Get(_ context.Context, ref Ref) (Credential, error) {
	secret, ok := e.lookup(ref.Value)
	if !ok {
		return Credential{}, fmt.Errorf("environment variable %s is not set (%s)", ref.Value, ref)
	}
	// An empty variable is reported separately from a missing one. Both are
	// mistakes, but they have different causes — unset means the variable
	// never reached the process, empty means something produced nothing — and
	// saying which saves an operator a round of guessing.
	if secret == "" {
		return Credential{}, fmt.Errorf("environment variable %s is set but empty (%s)", ref.Value, ref)
	}
	return Credential{Password: secret}, nil
}

func (e *Env) Store(context.Context, Ref, Credential) error {
	return fmt.Errorf("%w: env is a read-only credential source", ErrUnsupported)
}

func (e *Env) Delete(context.Context, Ref) error {
	return fmt.Errorf("%w: env is a read-only credential source", ErrUnsupported)
}

// File reads a secret from a file named by the reference, as in
// "file:/run/secrets/vcenter". One trailing newline is removed.
//
// This is the shape a systemd LoadCredential= unit, a Kubernetes secret
// mount, and a Docker secret all present. File permissions are deliberately
// not enforced: Kubernetes projects secret volumes world-readable inside the
// container by default, so refusing to read anything looser than 0600 would
// reject the most common way this scheme is used. Protecting the file is the
// operator's decision and the documentation says so.
type File struct{}

// NewFile returns a File provider.
func NewFile() *File { return &File{} }

func (f *File) Scheme() string { return SchemeFile }

func (f *File) Get(_ context.Context, ref Ref) (Credential, error) {
	fh, err := os.Open(ref.Value)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Credential{}, fmt.Errorf("credential file %s does not exist (%s)", ref.Value, ref)
		}
		return Credential{}, fmt.Errorf("read credential file %s: %w", ref.Value, err)
	}
	defer func() { _ = fh.Close() }()
	var buf bytes.Buffer
	// One byte past the ceiling, so going over is distinguishable from
	// landing exactly on it.
	if _, err := buf.ReadFrom(io.LimitReader(fh, maxSecretBytes+1)); err != nil {
		return Credential{}, fmt.Errorf("read credential file %s: %w", ref.Value, err)
	}
	if buf.Len() > maxSecretBytes {
		return Credential{}, fmt.Errorf("credential file %s is larger than %d bytes; that is not a password", ref.Value, maxSecretBytes)
	}
	secret := trimSecret(buf.Bytes())
	if secret == "" {
		return Credential{}, fmt.Errorf("credential file %s is empty (%s)", ref.Value, ref)
	}
	return Credential{Password: secret}, nil
}

func (f *File) Store(context.Context, Ref, Credential) error {
	return fmt.Errorf("%w: file is a read-only credential source", ErrUnsupported)
}

func (f *File) Delete(context.Context, Ref) error {
	return fmt.Errorf("%w: file is a read-only credential source", ErrUnsupported)
}

// Exec runs a program named by the reference and takes its standard output as
// the secret, as in "exec:/usr/local/bin/vsfleet-credential". One trailing
// newline is removed. It is how a secret manager vsfleet does not integrate
// with — Vault, pass, AWS Secrets Manager, an instance metadata token — is
// reached without vsfleet knowing anything about it.
//
// The reference names a program and nothing else: no arguments and no shell.
// Splitting a command string would mean choosing a quoting dialect and getting
// it subtly wrong in the one place where being wrong hands an attacker a shell,
// and a secret passed as an argument is visible to anyone who can run ps — the
// same reason the TUI refuses to put a proxy password on ssh's command line.
// A helper that needs arguments is a two-line wrapper script; the context name
// and the reference reach it through VSFLEET_CONTEXT and VSFLEET_CREDENTIAL_REF
// so one wrapper can serve an entire estate.
type Exec struct {
	// Environ overrides the environment passed to the helper. Tests set this.
	Environ func() []string
}

// NewExec returns an Exec provider.
func NewExec() *Exec { return &Exec{} }

func (e *Exec) Scheme() string { return SchemeExec }

func (e *Exec) environ() []string {
	if e.Environ != nil {
		return e.Environ()
	}
	return os.Environ()
}

// Get runs the helper. ctx carries the caller's per-context timeout, so a
// helper that hangs — waiting on a locked secret store, or on a network that
// is not there — fails the context it belongs to instead of the whole run.
func (e *Exec) Get(ctx context.Context, ref Ref) (Credential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	program := strings.TrimSpace(ref.Value)
	path, err := exec.LookPath(program)
	if err != nil {
		return Credential{}, fmt.Errorf("credential helper %s: %w", program, err)
	}
	cmd := exec.CommandContext(ctx, path)
	cmd.Env = append(e.environ(), EnvHelperRef+"="+ref.String())
	if label := labelFrom(ctx); label != "" {
		cmd.Env = append(cmd.Env, EnvHelperContext+"="+label)
	}
	// Cancelling the context kills the helper, but Wait would still block on
	// the output pipes until every process holding them closes — and a helper
	// that shells out has grandchildren that inherited them. WaitDelay bounds
	// that second wait, so a hung credential lookup fails its own context's
	// timeout instead of holding the whole capture open behind it.
	cmd.WaitDelay = helperWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// The helper inherits no standard input. It is not being asked a question,
	// and leaving it attached to the terminal would let a misbehaving one
	// swallow input meant for vsfleet.
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Credential{}, fmt.Errorf("credential helper %s did not finish: %w", program, ctxErr)
		}
		if msg := firstLine(stderr.String()); msg != "" {
			return Credential{}, fmt.Errorf("credential helper %s failed: %w: %s", program, err, msg)
		}
		return Credential{}, fmt.Errorf("credential helper %s failed: %w", program, err)
	}
	if stdout.Len() > maxSecretBytes {
		return Credential{}, fmt.Errorf("credential helper %s wrote more than %d bytes; that is not a password", program, maxSecretBytes)
	}
	secret := trimSecret(stdout.Bytes())
	if secret == "" {
		return Credential{}, fmt.Errorf("credential helper %s wrote nothing to standard output (%s)", program, ref)
	}
	return Credential{Password: secret}, nil
}

func (e *Exec) Store(context.Context, Ref, Credential) error {
	return fmt.Errorf("%w: exec is a read-only credential source", ErrUnsupported)
}

func (e *Exec) Delete(context.Context, Ref) error {
	return fmt.Errorf("%w: exec is a read-only credential source", ErrUnsupported)
}

// firstLine returns the first non-empty line of s, bounded so a helper that
// fails with a wall of output does not paste all of it into an error message.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		const max = 200
		if len(line) > max {
			return line[:max] + "…"
		}
		return line
	}
	return ""
}

// labelKey carries the context name a credential is being resolved for, so an
// exec: helper can be told which vCenter it is answering about. It travels on
// the context rather than on Ref because Ref is a map key and a config value:
// giving it a field that is neither would change both.
type labelKey struct{}

// WithLabel returns a context naming what a credential is being resolved for,
// normally the vsfleet context name.
func WithLabel(ctx context.Context, label string) context.Context {
	if label == "" {
		return ctx
	}
	return context.WithValue(ctx, labelKey{}, label)
}

func labelFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	label, _ := ctx.Value(labelKey{}).(string)
	return label
}
