package tests

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/vmware/govmomi/simulator"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/contextops"
	"github.com/easonliuuuuu/vsfleet/internal/credentials"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// staticResolver builds a credentials.Resolver over an in-memory keyring
// substitute, so contextops.Save can be exercised without touching the real
// OS keyring.
func staticResolver() *credentials.Resolver {
	return credentials.NewResolver(credentials.NewStatic(credentials.SchemeKeyring, nil))
}

// unavailableKeyring stands in for a machine with no OS secret store at all —
// a headless Linux box with no Secret Service, a locked-down container — the
// way go-keyring actually fails there: every call errors, not just Store.
type unavailableKeyring struct{ err error }

func (k unavailableKeyring) Scheme() string { return credentials.SchemeKeyring }
func (k unavailableKeyring) Get(context.Context, credentials.Ref) (credentials.Credential, error) {
	return credentials.Credential{}, k.err
}
func (k unavailableKeyring) Store(context.Context, credentials.Ref, credentials.Credential) error {
	return k.err
}
func (k unavailableKeyring) Delete(context.Context, credentials.Ref) error { return k.err }

// resolverWithNoKeyring builds a Resolver whose keyring provider always
// fails, and a prompt provider so the fallback this test proves has
// somewhere to land.
func resolverWithNoKeyring(promptAnswer string) *credentials.Resolver {
	err := errors.New("no secret service available")
	return credentials.NewResolver(
		unavailableKeyring{err: err},
		credentials.NewStatic(credentials.SchemePrompt, map[string]credentials.Credential{
			"": {Password: promptAnswer},
		}),
	)
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

// TestContextopsSaveFallsBackToPromptWhenTheKeyringIsUnavailable covers a
// machine with no OS password manager at all — no Secret Service, a locked
// container. Save must not fail outright, and the context it saves must be
// indistinguishable from one an operator configured with --credential
// prompt from the start: nothing in it should suggest a password lives
// somewhere it does not.
func TestContextopsSaveFallsBackToPromptWhenTheKeyringIsUnavailable(t *testing.T) {
	vc := startVCenter(t, func(m *simulator.Model) {})
	cfg := newCfg(t)
	resolver := resolverWithNoKeyring(testPassword)

	in := contextops.Input{
		Name: "prod", Endpoint: vc.URL, Username: "operator@vsphere.local",
		TLS:          config.TLSConfig{Mode: config.TLSThumbprint, Thumbprint: vc.Thumbprint},
		Password:     testPassword,
		HavePassword: true,
	}
	res, err := contextops.Save(context.Background(), cfg, resolver, vsphere.ConnectOptions{}, in, true)
	if err != nil {
		t.Fatalf("Save should still succeed when only the keyring write fails: %v (diagnosis: %+v)", err, res.Diagnosis)
	}
	if res.Diagnosis == nil || !res.Diagnosis.OK() {
		t.Fatalf("the connection test itself does not touch the keyring and should still pass, got %+v", res.Diagnosis)
	}
	if res.StoreWarning == nil {
		t.Error("expected a StoreWarning when the keyring is unavailable")
	}
	if res.Context.Credential.Scheme != credentials.SchemePrompt || res.Context.Credential.Value != "" {
		t.Errorf("credential = %q, want the bare prompt scheme with no leftover keyring value", res.Context.Credential)
	}

	saved, err := cfg.Context("prod")
	if err != nil {
		t.Fatalf("context was not saved: %v", err)
	}
	if saved.Credential.Scheme != credentials.SchemePrompt {
		t.Errorf("saved config credential = %q, want scheme %q", saved.Credential, credentials.SchemePrompt)
	}

	// Resolving it afterwards behaves exactly like a context configured with
	// --credential prompt from the start — the whole point of downgrading.
	cred, err := resolver.Get(context.Background(), saved.Credential)
	if err != nil {
		t.Fatalf("resolving the fallback prompt credential failed: %v", err)
	}
	if cred.Password != testPassword {
		t.Errorf("resolved password = %q, want %q", cred.Password, testPassword)
	}
}

// TestContextopsSaveProxyCredentialFallsBackToPromptToo checks the proxy's
// own password gets the identical treatment, independently of the vCenter's.
// It reuses testPassword for both rather than a second secret-shaped
// literal: the test compares each independently, not against each other, so
// nothing is lost by them being equal here.
func TestContextopsSaveProxyCredentialFallsBackToPromptToo(t *testing.T) {
	cfg := newCfg(t)
	resolver := resolverWithNoKeyring(testPassword)

	in := contextops.Input{
		Name: "via-proxy", Endpoint: "https://vcsa.internal", Username: "operator@vsphere.local",
		Transport: config.TransportConfig{Type: config.TransportSOCKS5, Address: "127.0.0.1:1080", Username: "svc-proxy"},
		TLS:       config.TLSConfig{Mode: config.TLSInsecure},

		Password:     testPassword,
		HavePassword: true,

		ProxyPassword:     testPassword,
		HaveProxyPassword: true,
	}
	// No connection test: this proves the Store fallback, not proxy
	// connectivity, and nothing here is actually listening on 127.0.0.1:1080.
	res, err := contextops.Save(context.Background(), cfg, resolver, vsphere.ConnectOptions{}, in, false)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if res.StoreWarning == nil {
		t.Error("expected a StoreWarning covering the proxy password too")
	}
	if res.Context.Transport.Credential.Scheme != credentials.SchemePrompt {
		t.Errorf("proxy credential = %q, want scheme %q", res.Context.Transport.Credential, credentials.SchemePrompt)
	}
	// The vCenter's own credential is an independent failure and must fall
	// back the same way, not be left pointing at a keyring entry that also
	// never got written.
	if res.Context.Credential.Scheme != credentials.SchemePrompt {
		t.Errorf("vCenter credential = %q, want scheme %q", res.Context.Credential, credentials.SchemePrompt)
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
