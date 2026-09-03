// Package tui implements the terminal user interface. It is deliberately the
// last layer built and the thinnest: everything it shows is available from the
// command line first, so the UI can be replaced without taking any behaviour
// with it.
package tui

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/contextops"
	"github.com/easonliuuuuu/vsfleet/internal/credentials"
	"github.com/easonliuuuuu/vsfleet/internal/session"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
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
	// BeginInventory connects if necessary and builds one context's shared
	// path index (see vsphere.Client.NewIndex), returning a handle that
	// every FetchGroup call for this same load reuses it through rather
	// than rebuilding it once per group. This is what lets the model
	// prioritize the visible kind's group and retrieve the rest
	// concurrently — see loadPriorityGroup and loadRemainingGroups.
	BeginInventory(ctx context.Context, cc *config.Context) (InventoryHandle, error)
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

// InventoryHandle is one context's connected, index-built inventory
// operation, returned by Backend.BeginInventory. Every FetchGroup call made
// through it reuses the same shared vsphere.Index rather than rebuilding it,
// and is safe to call concurrently with itself for different groups on the
// same handle — which is exactly how the model drives it: the priority
// group alone first, then the rest together once it lands.
type InventoryHandle interface {
	FetchGroup(group vsphere.FetchGroup) *vsphere.Inventory
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

func (b *sessionBackend) BeginInventory(ctx context.Context, cc *config.Context) (InventoryHandle, error) {
	// One deadline covers connecting, building the index and every group
	// fetched through the handle: without it, a vCenter that connects
	// quickly but hangs enumerating would run for as long as the interface
	// itself does, since ctx here is the program's whole-run context rather
	// than anything bounded by --timeout. sessionInventoryHandle releases it
	// once every fetch group has landed — see its FetchGroup.
	opCtx, cancel, tracker := b.mgr.Operation(ctx)
	s, err := b.mgr.Connect(opCtx, cc)
	if err != nil {
		cancel()
		return nil, b.mgr.TimeoutError(err, tracker)
	}
	client := s.Client()
	if client == nil {
		cancel()
		return nil, fmt.Errorf("context %q is not connected", cc.Name)
	}
	idx, err := client.NewIndex(opCtx)
	if err != nil {
		cancel()
		return nil, b.mgr.TimeoutError(err, tracker)
	}
	h := &sessionInventoryHandle{client: client, idx: idx, ctx: opCtx, cancel: cancel}
	h.remaining.Store(int32(len(vsphere.AllGroups)))
	return h, nil
}

// sessionInventoryHandle is the production InventoryHandle. It owns the
// per-operation deadline BeginInventory created and cancels it itself once
// every fetch group has been retrieved — exactly len(vsphere.AllGroups)
// FetchGroup calls, always, since the model issues one for every group on
// every load — so the caller never has to track that lifecycle separately.
type sessionInventoryHandle struct {
	client *vsphere.Client
	idx    *vsphere.Index
	ctx    context.Context
	cancel context.CancelFunc

	remaining atomic.Int32
}

func (h *sessionInventoryHandle) FetchGroup(group vsphere.FetchGroup) *vsphere.Inventory {
	inv := h.client.FetchGroup(h.ctx, h.idx, group)
	if h.remaining.Add(-1) == 0 {
		h.cancel()
	}
	return inv
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
	res, err := contextops.Save(ctx, b.cfg, b.res, b.opts, in, test)
	if err != nil {
		return res, err
	}
	// Editing a context leaves a session connected to what its name used to
	// mean. Manager.Connect would notice the configuration changed, but not a
	// password stored under an unchanged credential reference — and either way
	// the old login is worth ending now rather than at exit. A failure to log
	// out politely is not the operator's problem: the context is saved, and
	// the session is gone from the manager regardless.
	if in.Replace {
		_ = b.mgr.Forget(ctx, res.Context.Name)
	}
	return res, nil
}

func (b *sessionBackend) RemoveContext(ctx context.Context, name string, alsoCredential bool) (*config.Context, error) {
	cc, err := contextops.Remove(b.cfg, name)
	if err != nil {
		return nil, err
	}
	// A context that is gone from the configuration must not keep a login open
	// on the vCenter, nor keep reporting a status under a name that no longer
	// exists.
	_ = b.mgr.Forget(ctx, cc.Name)
	if alsoCredential {
		if err := contextops.DeleteCredentials(ctx, b.res, cc); err != nil {
			return cc, fmt.Errorf("context %q removed, but could not delete its stored password(s): %w", cc.Name, err)
		}
	}
	return cc, nil
}

func (b *sessionBackend) DiscoverThumbprint(ctx context.Context, cc *config.Context) (sha256, sha1, subject string, notAfter time.Time, err error) {
	return vsphere.FetchThumbprint(ctx, cc, b.opts)
}
