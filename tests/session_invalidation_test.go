package tests

import (
	"context"
	"testing"

	"github.com/vmware/govmomi/simulator"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/contextops"
	"github.com/easonliuuuuu/vsfleet/internal/session"
	"github.com/easonliuuuuu/vsfleet/internal/tui"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// fetchWholeInventory drives Backend.BeginInventory and every fetch group
// through to completion, assembling the whole bundle the way vsphere.Client
// itself does for a plain ListInventory. The interface itself never does
// this — it prioritizes one group and fetches the rest concurrently — but
// these tests want one full read to compare against a control connection,
// not to exercise that scheduling.
func fetchWholeInventory(ctx context.Context, backend tui.Backend, cc *config.Context) (*vsphere.Inventory, error) {
	handle, err := backend.BeginInventory(ctx, cc)
	if err != nil {
		return nil, err
	}
	inv := &vsphere.Inventory{Context: cc.Name}
	for _, g := range vsphere.AllGroups {
		inv.ApplyGroup(g, handle.FetchGroup(g, nil))
	}
	return inv, nil
}

// TestEditedContextTalksToTheNewVCenter pins the rule that a context's name is
// not its identity: editing a connected context to point somewhere else must
// reach the new vCenter, not keep answering from the connection the old
// endpoint left behind.
func TestEditedContextTalksToTheNewVCenter(t *testing.T) {
	a := startVCenter(t, func(m *simulator.Model) { m.Datacenter = 1; m.Machine = 2 })
	b := startVCenter(t, func(m *simulator.Model) { m.Datacenter = 1; m.Machine = 7 })

	cfg := newCfg(t)
	resolver := staticResolver()
	mgr := session.New(resolver)
	backend := tui.NewBackend(cfg, resolver, mgr, vsphere.ConnectOptions{Resolver: resolver})
	ctx := context.Background()

	in := contextops.Input{
		Name: "prod", Endpoint: a.URL, Username: "operator@vsphere.local",
		TLS:          config.TLSConfig{Mode: config.TLSThumbprint, Thumbprint: a.Thumbprint},
		Password:     testPassword,
		HavePassword: true,
		SetCurrent:   true,
	}
	if _, err := backend.SaveContext(ctx, in, true); err != nil {
		t.Fatalf("save the initial context: %v", err)
	}

	cc, err := cfg.Context("prod")
	if err != nil {
		t.Fatalf("context prod: %v", err)
	}
	first, err := fetchWholeInventory(ctx, backend, cc)
	if err != nil {
		t.Fatalf("inventory of the first vCenter: %v", err)
	}
	wantA := len(first.VMs) + len(first.Templates)

	// Edit the same context to point at an entirely different vCenter.
	in.Endpoint = b.URL
	in.TLS = config.TLSConfig{Mode: config.TLSThumbprint, Thumbprint: b.Thumbprint}
	in.Replace = true
	if _, err := backend.SaveContext(ctx, in, true); err != nil {
		t.Fatalf("save the edited context: %v", err)
	}

	cc, err = cfg.Context("prod")
	if err != nil {
		t.Fatalf("context prod after the edit: %v", err)
	}
	second, err := fetchWholeInventory(ctx, backend, cc)
	if err != nil {
		t.Fatalf("inventory after the edit: %v", err)
	}
	gotB := len(second.VMs) + len(second.Templates)

	// What the second vCenter actually holds, read over a connection that has
	// never been anywhere else.
	control := &config.Context{
		Name: "control", Endpoint: b.URL, Username: "operator@vsphere.local",
		Credential: cc.Credential,
		TLS:        config.TLSConfig{Mode: config.TLSThumbprint, Thumbprint: b.Thumbprint},
	}
	control.Normalize()
	controlSession, err := session.New(resolver).Connect(ctx, control)
	if err != nil {
		t.Fatalf("control connection to the second vCenter: %v", err)
	}
	controlInv, err := controlSession.Client().ListInventory(ctx)
	if err != nil {
		t.Fatalf("control inventory: %v", err)
	}
	wantB := len(controlInv.VMs) + len(controlInv.Templates)
	if wantA == wantB {
		t.Fatalf("the two simulated vCenters are indistinguishable (%d objects each): the test proves nothing", wantA)
	}
	if gotB != wantB {
		t.Errorf("after editing the endpoint the inventory has %d objects, want the new vCenter's %d (the old one had %d): the session was reused", gotB, wantB, wantA)
	}

	st, ok := backend.Status("prod")
	if !ok {
		t.Fatal("no session status for prod")
	}
	if st.Endpoint != cc.Endpoint {
		t.Errorf("session status endpoint = %q, want the edited %q", st.Endpoint, cc.Endpoint)
	}
}

// TestRemovedContextClosesItsSession pins the other half: a context that is
// gone from the configuration must not leave a live, logged-in client behind.
func TestRemovedContextClosesItsSession(t *testing.T) {
	vc := startVCenter(t, func(m *simulator.Model) { m.Datacenter = 1; m.Machine = 2 })

	cfg := newCfg(t)
	resolver := staticResolver()
	mgr := session.New(resolver)
	backend := tui.NewBackend(cfg, resolver, mgr, vsphere.ConnectOptions{Resolver: resolver})
	ctx := context.Background()

	in := contextops.Input{
		Name: "prod", Endpoint: vc.URL, Username: "operator@vsphere.local",
		TLS:          config.TLSConfig{Mode: config.TLSThumbprint, Thumbprint: vc.Thumbprint},
		Password:     testPassword,
		HavePassword: true,
		SetCurrent:   true,
	}
	if _, err := backend.SaveContext(ctx, in, true); err != nil {
		t.Fatalf("save: %v", err)
	}
	cc, err := cfg.Context("prod")
	if err != nil {
		t.Fatalf("context prod: %v", err)
	}
	if _, err := fetchWholeInventory(ctx, backend, cc); err != nil {
		t.Fatalf("inventory: %v", err)
	}

	if _, err := backend.RemoveContext(ctx, "prod", false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if st, ok := backend.Status("prod"); ok && st.State == session.Connected {
		t.Errorf("removed context still has a connected session: %+v", st)
	}
}
