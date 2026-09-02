// Package tui implements the terminal user interface. It is deliberately the
// last layer built and the thinnest: everything it shows is available from the
// command line first, so the UI can be replaced without taking any behaviour
// with it.
package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/easonliuuuuu/vc-tui/internal/config"
	"github.com/easonliuuuuu/vc-tui/internal/contextops"
	"github.com/easonliuuuuu/vc-tui/internal/credentials"
	"github.com/easonliuuuuu/vc-tui/internal/session"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

// Backend is everything the interface needs from the rest of the program.
//
// It exists as an interface for one reason: a UI that can only be exercised
// against a real vCenter is a UI nobody refactors. With this seam the whole
// model — tabs, selection, filtering, failure rendering, and the context
// setup form — is driven in tests by a fake that answers instantly.
type Backend interface {
	// Contexts lists the configured vCenters in display order. It reflects
	// the configuration as it stands right now, so it changes after
	// SaveContext or RemoveContext without needing to be re-fetched.
	Contexts() []*config.Context
	// Inventory connects if necessary and enumerates one vCenter.
	Inventory(ctx context.Context, cc *config.Context) (*vsphere.Inventory, error)
	// Status reports the connection state of one context. The second result
	// is false when no connection has been attempted yet.
	Status(name string) (session.Status, bool)
	// Diagnose walks the connection stages for one context, for the panel
	// that answers "why is this one red".
	Diagnose(ctx context.Context, cc *config.Context) *vsphere.Diagnosis

	// TestContext walks the connection for a context that has not been saved
	// yet, so the form can show whether it works before committing to it.
	TestContext(ctx context.Context, in contextops.Input) (*config.Context, *vsphere.Diagnosis)
	// SaveContext validates, optionally tests, and writes a context — adding
	// it or, when in.Replace is set, replacing the one of the same name.
	SaveContext(ctx context.Context, in contextops.Input, test bool) (*contextops.Result, error)
	// RemoveContext deletes a context and, when alsoCredential is set, its
	// stored password. It returns the context as it was just before removal.
	RemoveContext(ctx context.Context, name string, alsoCredential bool) (*config.Context, error)
	// DiscoverThumbprint fetches the certificate an endpoint presents,
	// without verifying it against any policy — the trust-on-first-use step
	// behind pinning a certificate that has never been seen before.
	DiscoverThumbprint(ctx context.Context, cc *config.Context) (sha256, sha1, subject string, notAfter time.Time, err error)
}

// sessionBackend is the production Backend, over the same session manager,
// configuration and credential resolver the command line uses.
type sessionBackend struct {
	cfg  *config.Config
	res  *credentials.Resolver
	mgr  *session.Manager
	opts vsphere.ConnectOptions
}

// NewBackend returns a Backend backed by a live session manager and the
// configuration file, so saving or removing a context from the interface is
// the same write the command line makes.
func NewBackend(cfg *config.Config, res *credentials.Resolver, mgr *session.Manager, opts vsphere.ConnectOptions) Backend {
	return &sessionBackend{cfg: cfg, res: res, mgr: mgr, opts: opts}
}

func (b *sessionBackend) Contexts() []*config.Context { return b.cfg.Contexts }

func (b *sessionBackend) Inventory(ctx context.Context, cc *config.Context) (*vsphere.Inventory, error) {
	s, err := b.mgr.Connect(ctx, cc)
	if err != nil {
		return nil, err
	}
	client := s.Client()
	if client == nil {
		return nil, fmt.Errorf("context %q is not connected", cc.Name)
	}
	return client.ListInventory(ctx)
}

func (b *sessionBackend) Status(name string) (session.Status, bool) { return b.mgr.Status(name) }

func (b *sessionBackend) Diagnose(ctx context.Context, cc *config.Context) *vsphere.Diagnosis {
	d, client := vsphere.Diagnose(ctx, cc, b.opts)
	if client != nil {
		// The diagnosis owns its own connection; the session manager's
		// long-lived one is untouched by it.
		_ = client.Close(context.WithoutCancel(ctx))
	}
	return d
}

func (b *sessionBackend) TestContext(ctx context.Context, in contextops.Input) (*config.Context, *vsphere.Diagnosis) {
	return contextops.Test(ctx, in, b.opts)
}

func (b *sessionBackend) SaveContext(ctx context.Context, in contextops.Input, test bool) (*contextops.Result, error) {
	return contextops.Save(ctx, b.cfg, b.res, b.opts, in, test)
}

func (b *sessionBackend) RemoveContext(ctx context.Context, name string, alsoCredential bool) (*config.Context, error) {
	cc, err := contextops.Remove(b.cfg, name)
	if err != nil {
		return nil, err
	}
	if alsoCredential && cc.Credential.Scheme == credentials.SchemeKeyring {
		if err := contextops.DeleteCredential(ctx, b.res, cc.Credential); err != nil && !errors.Is(err, credentials.ErrNotFound) {
			return cc, fmt.Errorf("context %q removed, but could not delete its stored password: %w", cc.Name, err)
		}
	}
	return cc, nil
}

func (b *sessionBackend) DiscoverThumbprint(ctx context.Context, cc *config.Context) (sha256, sha1, subject string, notAfter time.Time, err error) {
	return vsphere.FetchThumbprint(ctx, cc, b.opts)
}
