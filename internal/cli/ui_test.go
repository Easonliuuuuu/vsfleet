package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/contextops"
	"github.com/easonliuuuuu/vsfleet/internal/credentials"
)

// countingPrompt answers "prompt" references from a fixed set of queued
// answers and counts how many times it was actually asked. A test that
// primes the resolver and then repeats the same lookup can use the count to
// prove the second lookup was served from the primed cache rather than
// reaching this provider again — asking twice for an answer only queued
// once would fail outright instead of silently repeating it.
type countingPrompt struct {
	calls   int
	answers map[string]string
}

func (p *countingPrompt) Scheme() string { return credentials.SchemePrompt }

func (p *countingPrompt) Get(_ context.Context, ref credentials.Ref) (credentials.Credential, error) {
	p.calls++
	pw, ok := p.answers[ref.Value]
	if !ok {
		return credentials.Credential{}, fmt.Errorf("no answer queued for %q", ref)
	}
	return credentials.Credential{Password: pw}, nil
}

func (p *countingPrompt) Store(context.Context, credentials.Ref, credentials.Credential) error {
	return errors.New("operation not supported by credential provider: prompt")
}

func (p *countingPrompt) Delete(context.Context, credentials.Ref) error {
	return errors.New("operation not supported by credential provider: prompt")
}

func newTestApp(resolver *credentials.Resolver) *App {
	return &App{resolver: resolver}
}

func buildContext(t *testing.T, in contextops.Input) *config.Context {
	t.Helper()
	cc := contextops.Build(in)
	if err := cc.Validate(); err != nil {
		t.Fatalf("invalid fixture context: %v", err)
	}
	return cc
}

// TestResolveStartupCredentialsPrimesAgainstADoubleAsk is the core promise of
// issue #27: the selected context's password is asked for once, before
// Bubble Tea starts, and the background load that reaches the very same
// context moments later — the first thing Model.Init does — must not ask a
// second time.
func TestResolveStartupCredentialsPrimesAgainstADoubleAsk(t *testing.T) {
	cc := buildContext(t, contextops.Input{
		Name: "lab", Endpoint: "https://vcsa.internal", Username: "operator@vsphere.local",
		Credential: credentials.Ref{Scheme: credentials.SchemePrompt},
	})

	prompt := &countingPrompt{answers: map[string]string{"lab": "s3cret"}}
	resolver := credentials.NewResolver(prompt)
	a := newTestApp(resolver)

	if err := resolveStartupCredentials(context.Background(), a, cc); err != nil {
		t.Fatalf("resolveStartupCredentials: %v", err)
	}
	if prompt.calls != 1 {
		t.Fatalf("prompt provider called %d times resolving once, want 1", prompt.calls)
	}

	// The background load that connects to "lab" resolves its credential the
	// same way vsphere.resolveCredential does: the same reference, labelled
	// the same way.
	ref := cc.Credential.WithDefaultLabel(cc.Name)
	cred, err := resolver.Get(context.Background(), ref)
	if err != nil {
		t.Fatalf("Get after priming: %v", err)
	}
	if cred.Password != "s3cret" {
		t.Errorf("primed password = %q, want s3cret", cred.Password)
	}
	if prompt.calls != 1 {
		t.Errorf("prompt provider called %d times total, want 1 — the second lookup should have come from the primed cache", prompt.calls)
	}
}

// TestResolveStartupCredentialsSkipsThePromptForAStoredCredential checks the
// common case: a context backed by the OS keyring resolves silently, with no
// prompt at all.
func TestResolveStartupCredentialsSkipsThePromptForAStoredCredential(t *testing.T) {
	cc := buildContext(t, contextops.Input{
		Name: "lab", Endpoint: "https://vcsa.internal", Username: "operator@vsphere.local",
		Credential: credentials.Ref{Scheme: credentials.SchemeKeyring, Value: "lab"},
	})

	keyring := credentials.NewStatic(credentials.SchemeKeyring, map[string]credentials.Credential{
		"lab": {Password: "stored-secret"},
	})
	prompt := &countingPrompt{}
	resolver := credentials.NewResolver(keyring, prompt)
	a := newTestApp(resolver)

	if err := resolveStartupCredentials(context.Background(), a, cc); err != nil {
		t.Fatalf("resolveStartupCredentials: %v", err)
	}
	if prompt.calls != 0 {
		t.Errorf("prompt provider called %d times for a stored keyring credential, want 0", prompt.calls)
	}
}

// TestResolveStartupCredentialsAlsoPrimesTheProxyCredential checks that a
// context reached through an authenticated proxy has both secrets resolved
// up front — the vCenter's own and the proxy's — each primed under its own
// reference so neither is asked for twice.
func TestResolveStartupCredentialsAlsoPrimesTheProxyCredential(t *testing.T) {
	cc := buildContext(t, contextops.Input{
		Name: "via-proxy", Endpoint: "https://vcsa.internal", Username: "operator@vsphere.local",
		Credential: credentials.Ref{Scheme: credentials.SchemeKeyring, Value: "via-proxy"},
		Transport: config.TransportConfig{
			Type: config.TransportSOCKS5, Address: "127.0.0.1:1080", Username: "svc-proxy",
		},
		ProxyCredential: credentials.Ref{Scheme: credentials.SchemePrompt, Value: "via-proxy-proxy"},
	})

	keyring := credentials.NewStatic(credentials.SchemeKeyring, map[string]credentials.Credential{
		"via-proxy": {Password: "vcenter-secret"},
	})
	prompt := &countingPrompt{answers: map[string]string{"via-proxy-proxy": "proxy-secret"}}
	resolver := credentials.NewResolver(keyring, prompt)
	a := newTestApp(resolver)

	if err := resolveStartupCredentials(context.Background(), a, cc); err != nil {
		t.Fatalf("resolveStartupCredentials: %v", err)
	}
	if prompt.calls != 1 {
		t.Fatalf("prompt provider called %d times resolving the proxy credential once, want 1", prompt.calls)
	}

	cred, err := resolver.Get(context.Background(), cc.Transport.Credential)
	if err != nil {
		t.Fatalf("Get proxy credential after priming: %v", err)
	}
	if cred.Password != "proxy-secret" {
		t.Errorf("primed proxy password = %q, want proxy-secret", cred.Password)
	}
	if prompt.calls != 1 {
		t.Errorf("prompt provider called %d times total, want 1 — the proxy re-lookup should have come from the primed cache", prompt.calls)
	}
}

// TestResolveStartupCredentialsDoesNotPrimeAnAmbiguousProxyPrompt checks the
// one case priming must not touch: a proxy credential left at the bare
// "prompt" scheme carries no value of its own to key the primed cache on,
// and priming it could hand this context's proxy password to a different
// context whose own proxy credential happens to be the same bare reference.
func TestResolveStartupCredentialsDoesNotPrimeAnAmbiguousProxyPrompt(t *testing.T) {
	cc := buildContext(t, contextops.Input{
		Name: "via-proxy", Endpoint: "https://vcsa.internal", Username: "operator@vsphere.local",
		Credential: credentials.Ref{Scheme: credentials.SchemeKeyring, Value: "via-proxy"},
		Transport: config.TransportConfig{
			Type: config.TransportSOCKS5, Address: "127.0.0.1:1080", Username: "svc-proxy",
		},
		// A bare prompt reference for the proxy: contextops.Build leaves it
		// exactly as given, unlike the vCenter credential's own default.
		ProxyCredential: credentials.Ref{Scheme: credentials.SchemePrompt},
	})
	if got := cc.Transport.Credential; got.Scheme != credentials.SchemePrompt || got.Value != "" {
		t.Fatalf("fixture proxy credential = %q, want a bare prompt reference", got)
	}

	keyring := credentials.NewStatic(credentials.SchemeKeyring, map[string]credentials.Credential{
		"via-proxy": {Password: "vcenter-secret"},
	})
	prompt := &countingPrompt{answers: map[string]string{"": "proxy-secret"}}
	resolver := credentials.NewResolver(keyring, prompt)
	a := newTestApp(resolver)

	if err := resolveStartupCredentials(context.Background(), a, cc); err != nil {
		t.Fatalf("resolveStartupCredentials: %v", err)
	}
	if prompt.calls != 1 {
		t.Fatalf("prompt provider called %d times resolving the proxy credential once, want 1", prompt.calls)
	}

	// A second lookup for the same bare reference must reach the prompt
	// provider again — it was deliberately left unprimed.
	if _, err := resolver.Get(context.Background(), cc.Transport.Credential); err != nil {
		t.Fatalf("Get after (non-)priming: %v", err)
	}
	if prompt.calls != 2 {
		t.Errorf("prompt provider called %d times total, want 2 — an ambiguous bare-prompt proxy reference must not be primed", prompt.calls)
	}
}

// TestResolveStartupCredentialsFailsWithoutOpeningTheInterface checks that a
// credential failure at start-up is returned as a plain error — the caller,
// runUI, must return before ever calling tui.Run.
func TestResolveStartupCredentialsFailsWithoutOpeningTheInterface(t *testing.T) {
	cc := buildContext(t, contextops.Input{
		Name: "lab", Endpoint: "https://vcsa.internal", Username: "operator@vsphere.local",
		Credential: credentials.Ref{Scheme: credentials.SchemeKeyring, Value: "lab"},
	})

	keyring := credentials.NewStatic(credentials.SchemeKeyring, nil) // nothing stored
	resolver := credentials.NewResolver(keyring)                     // no prompt provider registered
	a := newTestApp(resolver)

	err := resolveStartupCredentials(context.Background(), a, cc)
	if err == nil {
		t.Fatal("resolveStartupCredentials should have failed: nothing is stored and there is no prompt to fall back to")
	}
}

// TestStartingContext checks the fallback chain: current when it names a
// real context, otherwise the first configured one, and nil with nothing
// configured — the one case there is no context to resolve credentials for.
func TestStartingContext(t *testing.T) {
	cc1 := buildContext(t, contextops.Input{Name: "prod", Endpoint: "https://a.internal", Username: "u"})
	cc2 := buildContext(t, contextops.Input{Name: "lab", Endpoint: "https://b.internal", Username: "u"})

	empty := &config.Config{}
	if got := startingContext(empty, ""); got != nil {
		t.Errorf("startingContext on an empty config = %v, want nil", got)
	}

	cfg := &config.Config{Contexts: []*config.Context{cc1, cc2}}
	if got := startingContext(cfg, "lab"); got != cc2 {
		t.Errorf("startingContext(%q) = %v, want the lab context", "lab", got)
	}
	if got := startingContext(cfg, ""); got != cc1 {
		t.Errorf("startingContext(\"\") = %v, want the first configured context", got)
	}
	if got := startingContext(cfg, "does-not-exist"); got != cc1 {
		t.Errorf("startingContext of an unknown name = %v, want the first configured context", got)
	}
}
