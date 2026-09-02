// Package tui implements the terminal user interface. It is deliberately the
// last layer built and the thinnest: everything it shows is available from the
// command line first, so the UI can be replaced without taking any behaviour
// with it.
package tui

import (
	"context"
	"fmt"

	"github.com/easonliuuuuu/vc-tui/internal/config"
	"github.com/easonliuuuuu/vc-tui/internal/session"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

// Backend is everything the interface needs from the rest of the program.
//
// It exists as an interface for one reason: a UI that can only be exercised
// against a real vCenter is a UI nobody refactors. With this seam the whole
// model — tabs, selection, filtering, failure rendering — is driven in tests
// by a fake that answers instantly.
type Backend interface {
	// Contexts lists the configured vCenters in display order.
	Contexts() []*config.Context
	// Inventory connects if necessary and enumerates one vCenter.
	Inventory(ctx context.Context, cc *config.Context) (*vsphere.Inventory, error)
	// Status reports the connection state of one context. The second result
	// is false when no connection has been attempted yet.
	Status(name string) (session.Status, bool)
	// Diagnose walks the connection stages for one context, for the panel
	// that answers "why is this one red".
	Diagnose(ctx context.Context, cc *config.Context) *vsphere.Diagnosis
}

// sessionBackend is the production Backend, over the same session manager the
// command line uses.
type sessionBackend struct {
	contexts []*config.Context
	mgr      *session.Manager
	opts     vsphere.ConnectOptions
}

// NewBackend returns a Backend backed by a live session manager.
func NewBackend(contexts []*config.Context, mgr *session.Manager, opts vsphere.ConnectOptions) Backend {
	return &sessionBackend{contexts: contexts, mgr: mgr, opts: opts}
}

func (b *sessionBackend) Contexts() []*config.Context { return b.contexts }

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
