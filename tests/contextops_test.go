package tests

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/vmware/govmomi/simulator"

	"github.com/easonliuuuuu/vc-tui/internal/config"
	"github.com/easonliuuuuu/vc-tui/internal/contextops"
	"github.com/easonliuuuuu/vc-tui/internal/credentials"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

// staticResolver builds a credentials.Resolver over an in-memory keyring
// substitute, so contextops.Save can be exercised without touching the real
// OS keyring.
func staticResolver() *credentials.Resolver {
	return credentials.NewResolver(credentials.NewStatic(credentials.SchemeKeyring, nil))
}

func newCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func TestContextopsSaveTestsAndStoresTheCredential(t *testing.T) {
	vc := startVCenter(t, func(m *simulator.Model) {})
	cfg := newCfg(t)
	resolver := staticResolver()

	in := contextops.Input{
		Name: "prod", Endpoint: vc.URL, Username: "operator@vsphere.local",
		TLS:          config.TLSConfig{Mode: config.TLSThumbprint, Thumbprint: vc.Thumbprint},
		Password:     "correct-horse",
		HavePassword: true,
		SetCurrent:   true,
	}
	res, err := contextops.Save(context.Background(), cfg, resolver, vsphere.ConnectOptions{}, in, true)
	if err != nil {
		t.Fatalf("Save: %v (diagnosis: %+v)", err, res.Diagnosis)
	}
	if res.Diagnosis == nil || !res.Diagnosis.OK() {
		t.Fatalf("expected a passing diagnosis, got %+v", res.Diagnosis)
	}
	if res.StoreWarning != nil {
		t.Errorf("unexpected store warning: %v", res.StoreWarning)
	}
	if _, err := cfg.Context("prod"); err != nil {
		t.Errorf("context was not saved: %v", err)
	}
	if cfg.CurrentContext != "prod" {
		t.Errorf("current context is %q, want prod", cfg.CurrentContext)
	}
	cred, err := resolver.Get(context.Background(), res.Context.Credential)
	if err != nil {
		t.Fatalf("password was not stored: %v", err)
	}
	if cred.Password != "correct-horse" {
		t.Errorf("stored password is %q, want correct-horse", cred.Password)
	}
}

func TestContextopsSaveBlocksOnFailedTestUnlessOverridden(t *testing.T) {
	cfg := newCfg(t)
	resolver := staticResolver()

	// 127.0.0.1:1 refuses the connection immediately, which fails the
	// diagnosis at the TCP stage without depending on DNS or a timeout.
	in := contextops.Input{
		Name: "unreachable", Endpoint: "https://127.0.0.1:1", Username: "operator@vsphere.local",
		TLS: config.TLSConfig{Mode: config.TLSInsecure},
	}

	res, err := contextops.Save(context.Background(), cfg, resolver, vsphere.ConnectOptions{}, in, true)
	if err == nil {
		t.Fatal("Save should have failed: the connection test never passes")
	}
	if res.Diagnosis == nil || res.Diagnosis.OK() {
		t.Fatalf("expected a failing diagnosis, got %+v", res.Diagnosis)
	}
	if _, cerr := cfg.Context("unreachable"); cerr == nil {
		t.Error("context should not have been saved after a failed test")
	}

	in.SaveOnTestFailure = true
	res, err = contextops.Save(context.Background(), cfg, resolver, vsphere.ConnectOptions{}, in, true)
	if err != nil {
		t.Fatalf("Save with SaveOnTestFailure should succeed despite the failing test: %v", err)
	}
	if res.Diagnosis == nil || res.Diagnosis.OK() {
		t.Errorf("SaveOnTestFailure should still report the real diagnosis, got %+v", res.Diagnosis)
	}
	if _, cerr := cfg.Context("unreachable"); cerr != nil {
		t.Errorf("context should have been saved: %v", cerr)
	}
}

func TestContextopsRemove(t *testing.T) {
	vc := startVCenter(t, func(m *simulator.Model) {})
	cfg := newCfg(t)
	resolver := staticResolver()

	in := contextops.Input{
		Name: "lab", Endpoint: vc.URL, Username: "operator@vsphere.local",
		TLS:          config.TLSConfig{Mode: config.TLSThumbprint, Thumbprint: vc.Thumbprint},
		Password:     "correct-horse",
		HavePassword: true,
		SetCurrent:   true,
	}
	res, err := contextops.Save(context.Background(), cfg, resolver, vsphere.ConnectOptions{}, in, true)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	ref := res.Context.Credential

	removed, err := contextops.Remove(cfg, "lab")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if removed.Name != "lab" {
		t.Errorf("Remove returned context %q, want lab", removed.Name)
	}
	if _, err := cfg.Context("lab"); err == nil {
		t.Error("context should be gone after Remove")
	}
	if cfg.CurrentContext != "" {
		t.Errorf("current context should be cleared, got %q", cfg.CurrentContext)
	}

	if err := contextops.DeleteCredential(context.Background(), resolver, ref); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}
	if _, err := resolver.Get(context.Background(), ref); !errors.Is(err, credentials.ErrNotFound) {
		t.Errorf("credential should be gone, Get returned: %v", err)
	}

	if _, err := contextops.Remove(cfg, "does-not-exist"); !errors.Is(err, config.ErrNotFound) {
		t.Errorf("Remove of an unknown context returned %v, want config.ErrNotFound", err)
	}
}
