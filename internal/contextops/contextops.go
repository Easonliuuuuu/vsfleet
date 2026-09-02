// Package contextops is where a context actually gets created, tested, saved
// and removed. The CLI wizard and the terminal interface both collect the
// same fields through different input methods — one over stdin prompts, one
// over a form — and both hand the result to this package rather than each
// re-implementing what "save a context" means.
package contextops

import (
	"context"
	"errors"
	"fmt"

	"github.com/easonliuuuuu/vc-tui/internal/config"
	"github.com/easonliuuuuu/vc-tui/internal/credentials"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

// Input is every field a context needs, already resolved to concrete values.
// Gathering them — prompting on a terminal, reading form fields — is the
// front end's job; this package never asks a question.
type Input struct {
	Name       string
	Endpoint   string
	Username   string
	Datacenter string
	Transport  config.TransportConfig
	TLS        config.TLSConfig
	// Credential is the reference to save. The zero value defaults to
	// keyring:<name>, which is what a new context gets when nothing else was
	// specified.
	Credential credentials.Ref
	// Password, when HavePassword is set, is stored under Credential (if it
	// is a keyring reference) and used for the connection test.
	Password     string
	HavePassword bool
	// Replace allows Save to overwrite an existing context of the same name,
	// which is what editing one means.
	Replace bool
	// SaveOnTestFailure keeps a context that failed its connection test,
	// for an operator who wants to fix the fault after saving rather than
	// before.
	SaveOnTestFailure bool
	// SetCurrent makes this the current context once saved.
	SetCurrent bool
}

// Build constructs and normalizes a context from Input without touching the
// configuration or the network. It is also what a "test before saving" action
// runs against.
func Build(in Input) *config.Context {
	cc := &config.Context{
		Name:       in.Name,
		Endpoint:   in.Endpoint,
		Username:   in.Username,
		Datacenter: in.Datacenter,
		Transport:  in.Transport,
		TLS:        in.TLS,
	}
	cc.Normalize()
	cred := in.Credential
	if cred.IsZero() {
		cred = credentials.Ref{Scheme: credentials.SchemeKeyring, Value: cc.Name}
	}
	cc.Credential = cred
	return cc
}

func connectOptions(connOpts vsphere.ConnectOptions, in Input) vsphere.ConnectOptions {
	if in.HavePassword {
		c := credentials.Credential{Password: in.Password}
		connOpts.Credential = &c
	}
	return connOpts
}

// Test builds a context from Input and walks its connection, without saving
// anything. It is what a "test connection" action runs before an operator
// commits to a context, and what Save runs again just before it writes.
func Test(ctx context.Context, in Input, connOpts vsphere.ConnectOptions) (*config.Context, *vsphere.Diagnosis) {
	cc := Build(in)
	if err := cc.Validate(); err != nil {
		return cc, &vsphere.Diagnosis{
			Context: cc.Name, Endpoint: cc.Endpoint, Route: cc.Transport.Describe(), TLS: cc.TLS.Describe(),
			Checks: []vsphere.Check{{Name: "Configuration valid", Status: vsphere.CheckFail, Err: err}},
		}
	}
	d, client := vsphere.Diagnose(ctx, cc, connectOptions(connOpts, in))
	if client != nil {
		_ = client.Close(context.WithoutCancel(ctx))
	}
	return cc, d
}

// Result is the outcome of Save: the context as written (or as it would have
// been), the diagnosis if a test ran, and a non-fatal warning when the
// context was saved but its password could not be stored.
type Result struct {
	Context      *config.Context
	Diagnosis    *vsphere.Diagnosis
	StoreWarning error
}

// Save validates Input, optionally tests the connection, stores the password
// and writes the context to the configuration — the one place both front ends
// go through, so "what does saving a context actually do" has one answer.
//
// On a validation error or a failed test that SaveOnTestFailure does not
// override, the context is not written and Save returns a non-nil error;
// Result is still populated so the caller can show what was attempted.
func Save(ctx context.Context, cfg *config.Config, resolver *credentials.Resolver, connOpts vsphere.ConnectOptions, in Input, test bool) (*Result, error) {
	cc := Build(in)
	res := &Result{Context: cc}
	if err := cc.Validate(); err != nil {
		return res, err
	}
	if test {
		var client *vsphere.Client
		res.Diagnosis, client = vsphere.Diagnose(ctx, cc, connectOptions(connOpts, in))
		if client != nil {
			_ = client.Close(context.WithoutCancel(ctx))
		}
		if !res.Diagnosis.OK() && !in.SaveOnTestFailure {
			if derr := res.Diagnosis.Err(); derr != nil {
				return res, fmt.Errorf("connection test failed: %w", derr)
			}
			return res, errors.New("connection test failed")
		}
	}
	if in.HavePassword && cc.Credential.Scheme == credentials.SchemeKeyring {
		if err := resolver.Store(ctx, cc.Credential, credentials.Credential{Password: in.Password}); err != nil {
			res.StoreWarning = err
		}
	}
	if err := cfg.Add(cc, in.Replace); err != nil {
		return res, err
	}
	if in.SetCurrent || cfg.CurrentContext == "" {
		cfg.CurrentContext = cc.Name
	}
	if err := cfg.Save(); err != nil {
		return res, err
	}
	return res, nil
}

// Remove deletes a context from the configuration and saves. It returns the
// removed context so the caller can decide whether to also delete its stored
// credential — that is a separate, explicit choice in both front ends, never
// a side effect of removing the context.
func Remove(cfg *config.Config, name string) (*config.Context, error) {
	cc, err := cfg.Context(name)
	if err != nil {
		return nil, err
	}
	if err := cfg.Remove(cc.Name); err != nil {
		return nil, err
	}
	if err := cfg.Save(); err != nil {
		return nil, err
	}
	return cc, nil
}

// DeleteCredential removes a context's stored password from its credential
// provider. Errors.Is(err, credentials.ErrNotFound) is expected and harmless
// when nothing was ever stored.
func DeleteCredential(ctx context.Context, resolver *credentials.Resolver, ref credentials.Ref) error {
	return resolver.Delete(ctx, ref)
}
